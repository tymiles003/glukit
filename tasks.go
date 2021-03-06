package main

import (
	"bufio"
	"github.com/alexandre-normand/glukit/app/engine"
	"github.com/alexandre-normand/glukit/app/importer"
	"github.com/alexandre-normand/glukit/app/model"
	"github.com/alexandre-normand/glukit/app/store"
	"github.com/alexandre-normand/glukit/app/util"
	"github.com/alexandre-normand/glukit/lib/drive"
	"github.com/alexandre-normand/glukit/lib/goauth2/oauth"
	"golang.org/x/net/context"
	"google.golang.org/appengine/channel"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/delay"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/taskqueue"
	"google.golang.org/appengine/urlfetch"
	"os"
	"time"
)

var processFile = delay.Func(PROCESS_FILE_FUNCTION_NAME, func(context context.Context, token *oauth.Token, file *drive.File, userEmail string,
	userProfileKey *datastore.Key) {
	log.Criticalf(context, "This function purely exists as a workaround to the \"initialization loop\" error that "+
		"shows up because the function calls a function that calls this one. This implementation defines the same signature as the "+
		"real one which we define in init() to override this implementation!")
})
var processDemoFile = delay.Func("processDemoFile", processStaticDemoFile)
var refreshUserData = delay.Func(REFRESH_USER_DATA_FUNCTION_NAME, func(context context.Context, userEmail string,
	autoScheduleNextRun bool) {
	log.Criticalf(context, "This function purely exists as a workaround to the \"initialization loop\" error that "+
		"shows up because the function calls itself. This implementation defines the same signature as the "+
		"real one which we define in init() to override this implementation!")
})

const (
	REFRESH_USER_DATA_FUNCTION_NAME = "refreshUserData"
	PROCESS_FILE_FUNCTION_NAME      = "processSingleFile"
	DATASTORE_WRITES_QUEUE_NAME     = "datastore-writes"
)

func disabledUpdateUserData(context context.Context, userEmail string, autoScheduleNextRun bool) {
	// noop
}

// updateUserData is an async task that searches on Google Drive for dexcom files. It handles some high
// watermark of the last import to avoid downloading already imported files (unless they've been updated).
// It also schedules itself to run again the next day unless the token is invalid.
func updateUserData(context context.Context, userEmail string, autoScheduleNextRun bool) {
	glukitUser, userProfileKey, _, err := store.GetUserData(context, userEmail)
	if _, ok := err.(store.StoreError); err != nil && !ok {
		log.Errorf(context, "We're trying to run an update data task for user [%s] that doesn't exist. "+
			"Got error: %v", userEmail, err)
		return
	}

	transport := &oauth.Transport{
		Config: configuration(),
		Transport: &urlfetch.Transport{
			Context: context,
		},
		Token: &glukitUser.Token,
	}

	// If the token is expired, try to get a fresh one by doing a refresh (which should use the refresh_token
	if glukitUser.Token.Expired() {
		transport.Token.RefreshToken = glukitUser.RefreshToken
		err := transport.Refresh(context)
		if err != nil {
			log.Errorf(context, "Error updating token for user [%s], let's hope he comes back soon so we can "+
				"get a fresh token: %v", userEmail, err)
			return
		}

		// Update the user with the new token
		log.Infof(context, "Token refreshed, updating user [%s] with token [%v]", userEmail, glukitUser.Token)
		store.StoreUserProfile(context, time.Now(), *glukitUser)
	}

	// Next update in one day
	nextUpdate := time.Now().AddDate(0, 0, 1)
	files, err := importer.SearchDataFiles(transport.Client(), glukitUser.MostRecentRead.GetTime())
	if err != nil {
		log.Warningf(context, "Error while searching for files on google drive for user [%s]: %v", userEmail, err)
	} else {
		switch {
		case len(files) == 0:
			log.Infof(context, "No new or updated data found for existing user [%s]", userEmail)
		case len(files) > 0:
			log.Infof(context, "Found new data files for user [%s], downloading and storing...", userEmail)
			processFileSearchResults(&glukitUser.Token, files, context, userEmail, userProfileKey)
		}
	}

	engine.StartGlukitScoreBatch(context, glukitUser)
	engine.StartA1CCalculationBatch(context, glukitUser)

	if autoScheduleNextRun {
		task, err := refreshUserData.Task(userEmail, autoScheduleNextRun)
		if err != nil {
			log.Criticalf(context, "Couldn't schedule the next execution of the data refresh for user [%s]. "+
				"This breaks background updating of user data!: %v", userEmail, err)
		}
		task.ETA = nextUpdate
		taskqueue.Add(context, task, "refresh")

		log.Infof(context, "Scheduled next data update for user [%s] at [%s]", userEmail, nextUpdate.Format(util.TIMEFORMAT))
	} else {
		log.Infof(context, "Not scheduling a the next refresh as requested by autoScheduleNextRun [%t]", autoScheduleNextRun)
	}
}

// processFileSearchResults reads the list of files detected on google drive and kicks off a new queued task
// to process each one
func processFileSearchResults(token *oauth.Token, files []*drive.File, context context.Context, userEmail string,
	userProfileKey *datastore.Key) {
	// TODO : Look at recent file import log for that file and skip to the new data. It would be nice to be able to
	// use the Http Range header but that's unlikely to be possible since new event/read data is spreadout in the
	// file
	for i := range files {
		enqueueFileImport(context, token, files[i], userEmail, userProfileKey, time.Duration(0))
	}
}

func enqueueFileImport(context context.Context, token *oauth.Token, file *drive.File, userEmail string, userKey *datastore.Key, delay time.Duration) error {
	log.Debugf(context, "Enqueuing import of file [%v] in %v", file, delay)

	task, err := processFile.Task(token, file, userEmail, userKey)
	if err != nil {
		return err
	}

	task.ETA = time.Now().Add(delay)
	_, err = taskqueue.Add(context, task, DATASTORE_WRITES_QUEUE_NAME)

	return err
}

// processSingleFile handles the import of a single file. It deals with:
//    1. Logging the file import operation
//    2. Calculating and updating the new GlukitScore
//    3. Sending a "refresh" message to any connected client
func processSingleFile(context context.Context, token *oauth.Token, file *drive.File, userEmail string,
	userProfileKey *datastore.Key) {
	t := &oauth.Transport{
		Config: configuration(),
		Transport: &urlfetch.Transport{
			Context: context,
		},
		Token: token,
	}

	reader, err := importer.GetFileReader(context, t, file)
	if err != nil {
		log.Infof(context, "Error reading file %s, skipping: [%v]", file.OriginalFilename, err)
	} else {
		// Default to beginning of time
		startTime := util.GLUKIT_EPOCH_TIME
		if lastFileImportLog, err := store.GetFileImportLog(context, userProfileKey, file.Id); err == nil {
			startTime = lastFileImportLog.LastDataProcessed
			log.Infof(context, "Reloading data from file [%s]-[%s] starting at date [%s]...", file.Id,
				file.OriginalFilename, startTime.Format(util.TIMEFORMAT))
		} else if err == datastore.ErrNoSuchEntity {
			log.Debugf(context, "First import of file [%s]-[%s]...", file.Id, file.OriginalFilename)
		} else if err != nil {
			util.Propagate(err)
		}

		lastReadTime, err := importer.ParseContent(context, reader, userProfileKey, startTime,
			store.StoreDaysOfReads, store.StoreDaysOfMeals, store.StoreDaysOfInjections, store.StoreDaysOfExercises)
		errMessage := "Success"
		if err != nil {
			enqueueFileImport(context, token, file, userEmail, userProfileKey, time.Duration(1)*time.Hour)
			errMessage = err.Error()
		}

		store.LogFileImport(context, userProfileKey, model.FileImportLog{Id: file.Id, Md5Checksum: file.Md5Checksum,
			LastDataProcessed: lastReadTime, ImportResult: errMessage})
		reader.Close()

		if err == nil {
			if glukitUser, err := store.GetUserProfile(context, userProfileKey); err != nil {
				log.Warningf(context, "Error getting retrieving GlukitUser [%s], this needs attention: [%v]", userEmail, err)
			} else {
				// Calculate Glukit Score batch here for the newly imported data
				err := engine.StartGlukitScoreBatch(context, glukitUser)
				if err != nil {
					log.Warningf(context, "Error starting batch calculation of GlukitScores for [%s], this needs attention: [%v]", userEmail, err)
				}

				err = engine.StartA1CCalculationBatch(context, glukitUser)
				if err != nil {
					log.Warningf(context, "Error starting a1c calculation batch for user [%s]: %v", userEmail, err)
				}
			}
		}
	}
	channel.Send(context, userEmail, "Refresh")
}

// processStaticDemoFile imports the static resource included with the app for the demo user
func processStaticDemoFile(context context.Context, userProfileKey *datastore.Key) {

	// open input file
	fi, err := os.Open("data.xml")
	if err != nil {
		panic(err)
	}
	// close fi on exit and check for its returned error
	defer func() {
		if fi.Close() != nil {
			panic(err)
		}
	}()
	// make a read buffer
	reader := bufio.NewReader(fi)

	lastReadTime, err := importer.ParseContent(context, reader, userProfileKey, util.GLUKIT_EPOCH_TIME,
		store.StoreDaysOfReads, store.StoreDaysOfMeals, store.StoreDaysOfInjections, store.StoreDaysOfExercises)

	if err != nil {
		util.Propagate(err)
	}

	store.LogFileImport(context, userProfileKey, model.FileImportLog{Id: "demo", Md5Checksum: "dummychecksum",
		LastDataProcessed: lastReadTime, ImportResult: "Success"})

	if userProfile, err := store.GetUserProfile(context, userProfileKey); err != nil {
		log.Warningf(context, "Error while persisting score for %s: %v", DEMO_EMAIL, err)
	} else {
		if err := engine.StartGlukitScoreBatch(context, userProfile); err != nil {
			log.Warningf(context, "Error while starting batch calculation of glukit scores for %s: %v", DEMO_EMAIL, err)
		}

		err = engine.StartA1CCalculationBatch(context, userProfile)
		if err != nil {
			log.Warningf(context, "Error starting a1c calculation batch for user [%s]: %v", DEMO_EMAIL, err)
		}
	}

	channel.Send(context, DEMO_EMAIL, "Refresh")
}
