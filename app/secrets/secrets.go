package secrets

//go:generate safekeeper --keys=LOCAL_CLIENT_ID,LOCAL_CLIENT_SECRET,PROD_CLIENT_ID,PROD_CLIENT_SECRET,TEST_STRIPE_KEY,TEST_STRIPE_PUBLISHABLE_KEY,PROD_STRIPE_KEY,PROD_STRIPE_PUBLISHABLE_KEY,GLUKLOADER_CLIENT_ID,GLUKLOADER_CLIENT_SECRET,POSTMAN_CLIENT_ID,POSTMAN_CLIENT_SECRET,SIMPLE_CLIENT_ID,SIMPLE_CLIENT_SECRET,CHROMADEX_CLIENT_ID,CHROMADEX_CLIENT_SECRET $GOFILE