package service

// WithLookupEnv sets the function container detection reads environment
// variables through, in place of os.LookupEnv.
func WithLookupEnv(lookup func(key string) (string, bool)) Option {
	return func(o *options) { o.lookupEnv = lookup }
}
