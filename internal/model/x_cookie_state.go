package model

// XCookieState records why an X cookie file is resting. Rate limits only set
// RetryAtMs; failed sessions also count toward dropping the file. Session
// identifies the login the state applies to.
type XCookieState struct {
	Label     string
	Session   string
	FailCount int
	RetryAtMs int64
	Dropped   bool
	LastError string
}
