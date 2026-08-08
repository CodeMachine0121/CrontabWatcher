package system

import "time"

// Clock 提供當下時間。
type Clock struct {
	location *time.Location
}

// NewClock 建立時鐘。location 決定回傳時間的時區 —— cron 的排程語意綁在時區上，
// 用 UTC 算出的「下次執行」會是錯的牆上時間。
func NewClock(location *time.Location) *Clock {
	return &Clock{location: location}
}

// Now 回傳當下時間，時區為建構時指定的那個。
func (clock *Clock) Now() time.Time {
	return time.Now().In(clock.location)
}
