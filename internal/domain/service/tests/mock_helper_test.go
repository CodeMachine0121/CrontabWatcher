package service_test

import "github.com/stretchr/testify/mock"

// mock_anySlice 讓斷言不必在意 jobID 的順序，用在順序無關的呼叫上。
func mock_anySlice() interface{} {
	return mock.Anything
}
