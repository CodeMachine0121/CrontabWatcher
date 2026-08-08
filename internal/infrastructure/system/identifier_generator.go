package system

import "github.com/google/uuid"

// IdentifierGenerator 產生 JobID 與 RunID。
type IdentifierGenerator struct{}

// NewIdentifierGenerator 建立產生器。
func NewIdentifierGenerator() *IdentifierGenerator {
	return &IdentifierGenerator{}
}

// NewIdentifier 產生一個隨機 UUID。
//
// 用隨機而非遞增：識別碼會被寫進使用者的 crontab 註解裡，遞增的號碼會讓兩份不同
// 機器的 crontab 出現同號的 job，合併時就撞在一起。
func (generator *IdentifierGenerator) NewIdentifier() string {
	return uuid.NewString()
}
