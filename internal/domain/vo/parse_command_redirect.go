package vo

import (
	"strings"
	"unicode"
)

// ParseCommandRedirect 把一個 crontab 指令切成「純指令」與「輸出重導向」兩半。
//
// 這是無狀態的純文字轉換，故為 package-level 函式而非某個型別的 method。
//
// 掃描時會追蹤引號狀態，因此 `echo 'a > b'` 裡的 > 不會被誤認成重導向 —— 這種
// 誤判會讓我們去讀一個根本不存在的檔案，並在 UI 上謊報 log 位置。
//
// **重導向不一定在指令尾端。** cron 條目常常是 `a && b >> log 2>&1 && c` 這種
// 串接，其中的重導向屬於串接中的某一段。這種情況下：
//   - 第一個回傳值是**完整的原指令**，一個字都不切掉。截斷後的指令若被手動觸發，
//     會只跑一半的 pipeline，那比不跑更糟。
//   - 重導向仍然回傳（那個檔案確實收得到輸出，是有用的 log），但 IsTrailing()
//     為 false，呼叫端據此知道不能把它從指令裡剝掉。
//
// 若指令沒有把輸出導向任何檔案（包含只寫了 2>&1 的情形），回傳的 redirect 為
// nil，且第一個回傳值與輸入完全相同。
func ParseCommandRedirect(command string) (string, *CommandRedirect) {
	redirectStartIndex, found := findRedirectSectionStart(command)
	if !found {
		return command, nil
	}

	commandBeforeRedirect := strings.TrimRight(command[:redirectStartIndex], " \t")
	redirectSection := command[len(commandBeforeRedirect):]

	parsed := parseRedirectSection(redirectSection)
	if !parsed.hasFileTarget {
		return command, nil
	}

	// 消耗完的重導向語法之後若還有非空白內容，代表這是串接指令的一部分，而不是
	// 整道指令的尾端重導向。
	isTrailing := strings.TrimSpace(redirectSection[parsed.consumedLength:]) == ""

	bareCommand := commandBeforeRedirect
	if !isTrailing {
		bareCommand = command
	}

	return bareCommand, &CommandRedirect{
		targetFilePath:        parsed.targetFilePath,
		appends:               parsed.appends,
		includesStandardError: parsed.includesStandardError,
		rawFragment:           redirectSection[:parsed.consumedLength],
		trailing:              isTrailing,
	}
}

// findRedirectSectionStart 找出第一個位於引號外的重導向運算子起始位置。
//
// 取「第一個」而非「最後一個」，有兩個理由：讓 `cmd 2>&1 >> /path` 這種把 stderr
// 合併寫在前面的形態也能被完整切出來；以及在串接指令裡，最前面那個重導向才是這個
// job 的 log，後面的多半是它自己的暫存檔。
func findRedirectSectionStart(command string) (int, bool) {
	insideSingleQuote := false
	insideDoubleQuote := false

	for index := 0; index < len(command); index++ {
		character := command[index]

		switch {
		case character == '\\' && !insideSingleQuote:
			index++ // 跳過被轉義的字元
			continue
		case character == '\'' && !insideDoubleQuote:
			insideSingleQuote = !insideSingleQuote
			continue
		case character == '"' && !insideSingleQuote:
			insideDoubleQuote = !insideDoubleQuote
			continue
		}

		if insideSingleQuote || insideDoubleQuote {
			continue
		}

		switch character {
		case '>':
			return index, true
		case '&':
			if index+1 < len(command) && command[index+1] == '>' {
				return index, true
			}
		case '0', '1', '2':
			// 檔案描述子只有在自成一個 token 時才算（前面是空白或字串開頭），
			// 否則 `/bin/report2 --daily` 的 2 會被誤讀成描述子。
			if index+1 < len(command) && command[index+1] == '>' && isTokenBoundary(command, index) {
				return index, true
			}
		}
	}

	return 0, false
}

func isTokenBoundary(command string, index int) bool {
	if index == 0 {
		return true
	}

	return unicode.IsSpace(rune(command[index-1]))
}

// parsedRedirectSection 是解析重導向區段的結果。
type parsedRedirectSection struct {
	targetFilePath        string
	appends               bool
	includesStandardError bool
	hasFileTarget         bool
	// consumedLength 是實際被辨識為重導向語法的字元數。之後若還有內容，代表這道
	// 指令是串接的。
	consumedLength int
}

// parseRedirectSection 解析重導向區段，找出實際被寫入的檔案與 stderr 的去向。
//
// **遇到第一個不是重導向運算子的 token 就停下來**，而不是掃到字串結尾。掃到底會
// 讓 `>> log 2>&1 && tail ... > /tmp/tmp` 裡的暫存檔覆蓋掉真正的 log 位置。
//
// stdout 的重導向優先作為 log 目標；若只有 stderr 被導向檔案，就用那個檔案。
func parseRedirectSection(section string) parsedRedirectSection {
	result := parsedRedirectSection{}

	standardErrorMergedIntoOutput := false
	standardErrorTargetFilePath := ""
	standardErrorAppends := false

	index := 0
	for index < len(section) {
		cursor := index
		for cursor < len(section) && unicode.IsSpace(rune(section[cursor])) {
			cursor++
		}
		if cursor >= len(section) {
			break
		}

		operator, fileDescriptor, operatorLength := readRedirectOperator(section, cursor)
		if operatorLength == 0 {
			break
		}

		cursor += operatorLength

		if operator == mergeStandardErrorOperator {
			standardErrorMergedIntoOutput = true
			index = cursor
			result.consumedLength = cursor
			continue
		}

		operand, operandLength := readOperand(section, cursor)
		if operand == "" {
			break
		}
		cursor += operandLength

		switch fileDescriptor {
		case standardErrorDescriptor:
			if standardErrorTargetFilePath == "" {
				standardErrorTargetFilePath = operand
				standardErrorAppends = operator == appendOperator
			}
		case bothStreamsDescriptor:
			if !result.hasFileTarget {
				result.targetFilePath = operand
				result.appends = operator == appendOperator
				result.includesStandardError = true
				result.hasFileTarget = true
			}
		default:
			if !result.hasFileTarget {
				result.targetFilePath = operand
				result.appends = operator == appendOperator
				result.hasFileTarget = true
			}
		}

		index = cursor
		result.consumedLength = cursor
	}

	if result.hasFileTarget {
		result.includesStandardError = result.includesStandardError || standardErrorMergedIntoOutput
		return result
	}

	if standardErrorTargetFilePath != "" {
		result.targetFilePath = standardErrorTargetFilePath
		result.appends = standardErrorAppends
		result.includesStandardError = true
		result.hasFileTarget = true
	}

	return result
}

type redirectOperator int

const (
	unknownOperator redirectOperator = iota
	truncateOperator
	appendOperator
	mergeStandardErrorOperator
)

type redirectDescriptor int

const (
	standardOutputDescriptor redirectDescriptor = iota
	standardErrorDescriptor
	bothStreamsDescriptor
)

// readRedirectOperator 讀出位於 index 的重導向運算子，回傳其種類、作用的檔案
// 描述子，以及消耗的字元數（0 表示此處不是運算子）。
func readRedirectOperator(section string, index int) (redirectOperator, redirectDescriptor, int) {
	remaining := section[index:]

	switch {
	case strings.HasPrefix(remaining, "2>&1"):
		return mergeStandardErrorOperator, standardErrorDescriptor, len("2>&1")
	case strings.HasPrefix(remaining, "&>>"):
		return appendOperator, bothStreamsDescriptor, len("&>>")
	case strings.HasPrefix(remaining, "&>"):
		return truncateOperator, bothStreamsDescriptor, len("&>")
	case strings.HasPrefix(remaining, "2>>"):
		return appendOperator, standardErrorDescriptor, len("2>>")
	case strings.HasPrefix(remaining, "2>"):
		return truncateOperator, standardErrorDescriptor, len("2>")
	case strings.HasPrefix(remaining, "1>>"):
		return appendOperator, standardOutputDescriptor, len("1>>")
	case strings.HasPrefix(remaining, "1>"):
		return truncateOperator, standardOutputDescriptor, len("1>")
	case strings.HasPrefix(remaining, ">>"):
		return appendOperator, standardOutputDescriptor, len(">>")
	case strings.HasPrefix(remaining, ">"):
		return truncateOperator, standardOutputDescriptor, len(">")
	}

	return unknownOperator, standardOutputDescriptor, 0
}

// readOperand 讀出運算子後面的檔案路徑，回傳路徑與消耗的字元數（含前導空白）。
func readOperand(section string, index int) (string, int) {
	cursor := index
	for cursor < len(section) && unicode.IsSpace(rune(section[cursor])) {
		cursor++
	}

	operandStart := cursor
	for cursor < len(section) && !unicode.IsSpace(rune(section[cursor])) {
		cursor++
	}

	return section[operandStart:cursor], cursor - index
}
