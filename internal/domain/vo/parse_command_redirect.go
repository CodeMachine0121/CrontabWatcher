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
// 若指令沒有把輸出導向任何檔案（包含只寫了 2>&1 的情形），回傳的 redirect 為
// nil，且第一個回傳值與輸入完全相同。
func ParseCommandRedirect(command string) (string, *CommandRedirect) {
	redirectStartIndex, found := findRedirectSectionStart(command)
	if !found {
		return command, nil
	}

	bareCommand := strings.TrimRight(command[:redirectStartIndex], " \t")
	redirectSection := command[len(bareCommand):]

	targetFilePath, appends, includesStandardError, hasFileTarget := parseRedirectSection(redirectSection)
	if !hasFileTarget {
		return command, nil
	}

	return bareCommand, &CommandRedirect{
		targetFilePath:        targetFilePath,
		appends:               appends,
		includesStandardError: includesStandardError,
		rawFragment:           redirectSection,
	}
}

// findRedirectSectionStart 找出第一個位於引號外的重導向運算子起始位置。
//
// 取「第一個」而非「最後一個」，是為了讓 `cmd 2>&1 >> /path` 這種把 stderr 合併
// 寫在前面的形態也能被完整切出來。
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

// parseRedirectSection 解析重導向區段，找出實際被寫入的檔案與 stderr 的去向。
//
// stdout 的重導向優先作為 log 目標；若只有 stderr 被導向檔案，就用那個檔案。
func parseRedirectSection(section string) (targetFilePath string, appends bool, includesStandardError bool, hasFileTarget bool) {
	standardErrorMergedIntoOutput := false
	standardErrorTargetFilePath := ""
	standardErrorAppends := false

	for index := 0; index < len(section); {
		if unicode.IsSpace(rune(section[index])) {
			index++
			continue
		}

		operator, fileDescriptor, operatorLength := readRedirectOperator(section, index)
		if operatorLength == 0 {
			index++
			continue
		}

		index += operatorLength

		if operator == mergeStandardErrorOperator {
			standardErrorMergedIntoOutput = true
			continue
		}

		operand, operandLength := readOperand(section, index)
		index += operandLength
		if operand == "" {
			continue
		}

		switch fileDescriptor {
		case standardErrorDescriptor:
			standardErrorTargetFilePath = operand
			standardErrorAppends = operator == appendOperator
		case bothStreamsDescriptor:
			targetFilePath = operand
			appends = operator == appendOperator
			hasFileTarget = true
			includesStandardError = true
		default:
			targetFilePath = operand
			appends = operator == appendOperator
			hasFileTarget = true
		}
	}

	if hasFileTarget {
		return targetFilePath, appends, includesStandardError || standardErrorMergedIntoOutput, true
	}

	if standardErrorTargetFilePath != "" {
		return standardErrorTargetFilePath, standardErrorAppends, true, true
	}

	return "", false, false, false
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
