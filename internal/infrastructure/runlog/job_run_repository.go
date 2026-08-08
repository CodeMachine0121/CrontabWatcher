package runlog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
)

const (
	recordFilePermission      = 0o600
	recordDirectoryPermission = 0o700
	// recordScannerMaxLineBytes 要容得下一筆含 8 KiB 輸出摘要的紀錄，留足餘裕。
	recordScannerMaxLineBytes = 1 << 20
)

// jobRunRecord 是 runs.jsonl 裡一行的形狀。
//
// finishedAt 與 exitCode 用指標，因為「未完成」與「exit code 0」必須能區分開來；
// 用零值表示未完成會把未知講成成功。
type jobRunRecord struct {
	RunID           string     `json:"runId"`
	JobID           string     `json:"jobId"`
	TriggerSource   string     `json:"triggerSource"`
	RunStatus       string     `json:"runStatus"`
	StartedAt       time.Time  `json:"startedAt"`
	FinishedAt      *time.Time `json:"finishedAt"`
	ExitCode        *int       `json:"exitCode"`
	OutputExcerpt   string     `json:"outputExcerpt"`
	OutputTruncated bool       `json:"outputTruncated"`
}

// JobRunRepository 以 append-only JSON Lines 持久化執行紀錄。
//
// 沒有資料庫是刻意的選擇：這個規模的資料量用一個檔案就夠，備份等於 cp，而且
// 掛在 volume 上就自動持久化。
type JobRunRepository struct {
	recordFilePath string
	retentionCount int
	writeMutex     sync.Mutex
}

// NewJobRunRepository 建立 repository。retentionCount 為每個 job 保留的紀錄筆數。
func NewJobRunRepository(recordFilePath string, retentionCount int) *JobRunRepository {
	return &JobRunRepository{
		recordFilePath: recordFilePath,
		retentionCount: retentionCount,
	}
}

// Append 加入一筆紀錄，必要時建立父目錄，並在超出保留筆數時壓縮檔案。
func (repository *JobRunRepository) Append(run *entity.JobRun) error {
	repository.writeMutex.Lock()
	defer repository.writeMutex.Unlock()

	if err := os.MkdirAll(filepath.Dir(repository.recordFilePath), recordDirectoryPermission); err != nil {
		return fmt.Errorf("creating run record directory: %w", err)
	}

	encodedRecord, err := json.Marshal(buildRecord(run))
	if err != nil {
		return fmt.Errorf("encoding run record %s: %w", run.RunID(), err)
	}

	file, err := os.OpenFile(repository.recordFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, recordFilePermission)
	if err != nil {
		return fmt.Errorf("opening run record file %s: %w", repository.recordFilePath, err)
	}

	if _, err := file.Write(append(encodedRecord, '\n')); err != nil {
		file.Close()
		return fmt.Errorf("appending run record %s: %w", run.RunID(), err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("closing run record file: %w", err)
	}

	return repository.compactIfNeeded(run.JobID())
}

// Update 以 RunID 覆寫既有紀錄。
//
// 整份重寫而非就地改寫：紀錄長度會變（輸出摘要在收尾時才填進去），就地改寫需要
// 定長欄位，那對一個人用的工具是過度設計。
func (repository *JobRunRepository) Update(run *entity.JobRun) error {
	repository.writeMutex.Lock()
	defer repository.writeMutex.Unlock()

	records, err := repository.readRecords()
	if err != nil {
		return err
	}

	replaced := false
	for index := range records {
		if records[index].RunID == run.RunID() {
			records[index] = buildRecord(run)
			replaced = true
			break
		}
	}

	if !replaced {
		return fmt.Errorf("%w: %s", entity.ErrJobRunNotFound, run.RunID())
	}

	return repository.rewriteRecords(records)
}

// ListByJobID 回傳該 job 的紀錄，新到舊。limit 為 0 或負值表示不限。
func (repository *JobRunRepository) ListByJobID(jobID string, limit int) ([]*entity.JobRun, error) {
	records, err := repository.readRecords()
	if err != nil {
		return nil, err
	}

	matchingRecords := filterRecordsByJobID(records, jobID)
	sortRecordsNewestFirst(matchingRecords)

	if limit > 0 && len(matchingRecords) > limit {
		matchingRecords = matchingRecords[:limit]
	}

	runs := make([]*entity.JobRun, 0, len(matchingRecords))
	for _, record := range matchingRecords {
		runs = append(runs, restoreRun(record))
	}

	return runs, nil
}

// LatestByJobIDs 一次取多個 job 各自最新的紀錄。沒有紀錄的 job 不會出現在 map 裡
// —— 對映到 nil 會讓呼叫端每次都要多檢查一層。
func (repository *JobRunRepository) LatestByJobIDs(jobIDs []string) (map[string]*entity.JobRun, error) {
	records, err := repository.readRecords()
	if err != nil {
		return nil, err
	}

	requestedJobIDs := make(map[string]bool, len(jobIDs))
	for _, jobID := range jobIDs {
		requestedJobIDs[jobID] = true
	}

	latestRecords := make(map[string]jobRunRecord)
	for _, record := range records {
		if !requestedJobIDs[record.JobID] {
			continue
		}

		existingRecord, found := latestRecords[record.JobID]
		if !found || record.StartedAt.After(existingRecord.StartedAt) {
			latestRecords[record.JobID] = record
		}
	}

	latestRuns := make(map[string]*entity.JobRun, len(latestRecords))
	for jobID, record := range latestRecords {
		latestRuns[jobID] = restoreRun(record)
	}

	return latestRuns, nil
}

// HasRunningRun 回報該 job 是否已有執行中的紀錄。
func (repository *JobRunRepository) HasRunningRun(jobID string) (bool, error) {
	records, err := repository.readRecords()
	if err != nil {
		return false, err
	}

	for _, record := range records {
		if record.JobID == jobID && entity.NewRunStatus(record.RunStatus) == entity.RunStatusRunning {
			return true, nil
		}
	}

	return false, nil
}

// MarkRunningRunsAsInterrupted 把殘留的 running 紀錄標成無法判定，回傳處理筆數。
//
// server 啟動時呼叫一次。留著它們假裝還在跑，會讓「這個 job 卡住了嗎」永遠問不出
// 答案，也會讓並發檢查永久擋住該 job 的手動觸發。
func (repository *JobRunRepository) MarkRunningRunsAsInterrupted(interruptedAt time.Time) (int, error) {
	repository.writeMutex.Lock()
	defer repository.writeMutex.Unlock()

	records, err := repository.readRecords()
	if err != nil {
		return 0, err
	}

	interruptedCount := 0
	for index, record := range records {
		if entity.NewRunStatus(record.RunStatus) != entity.RunStatusRunning {
			continue
		}

		run := restoreRun(record)
		run.MarkInterrupted(interruptedAt)
		records[index] = buildRecord(run)
		interruptedCount++
	}

	if interruptedCount == 0 {
		return 0, nil
	}

	if err := repository.rewriteRecords(records); err != nil {
		return 0, err
	}

	return interruptedCount, nil
}

// readRecords 讀出全部紀錄。
//
// 壞掉的行一律略過而非整批失敗：一次沒寫完的 append 或人為編輯都可能留下半行，
// 而其餘紀錄仍然完好，沒有理由因此讓整份歷史消失。
func (repository *JobRunRepository) readRecords() ([]jobRunRecord, error) {
	file, err := os.Open(repository.recordFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("opening run record file %s: %w", repository.recordFilePath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), recordScannerMaxLineBytes)

	records := make([]jobRunRecord, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var record jobRunRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		if record.RunID == "" {
			continue
		}

		records = append(records, record)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading run record file %s: %w", repository.recordFilePath, err)
	}

	return records, nil
}

// rewriteRecords 原子地重寫整份紀錄檔。
func (repository *JobRunRepository) rewriteRecords(records []jobRunRecord) error {
	directory := filepath.Dir(repository.recordFilePath)
	if err := os.MkdirAll(directory, recordDirectoryPermission); err != nil {
		return fmt.Errorf("creating run record directory: %w", err)
	}

	temporaryFile, err := os.CreateTemp(directory, ".runs-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temporary run record file: %w", err)
	}
	temporaryFilePath := temporaryFile.Name()

	writeErr := func() error {
		defer temporaryFile.Close()

		writer := bufio.NewWriter(temporaryFile)
		for _, record := range records {
			encodedRecord, err := json.Marshal(record)
			if err != nil {
				return fmt.Errorf("encoding run record %s: %w", record.RunID, err)
			}
			if _, err := writer.Write(append(encodedRecord, '\n')); err != nil {
				return fmt.Errorf("writing run record %s: %w", record.RunID, err)
			}
		}

		if err := writer.Flush(); err != nil {
			return fmt.Errorf("flushing run records: %w", err)
		}
		if err := temporaryFile.Chmod(recordFilePermission); err != nil {
			return fmt.Errorf("setting permissions on temporary run record file: %w", err)
		}

		return temporaryFile.Sync()
	}()

	if writeErr != nil {
		_ = os.Remove(temporaryFilePath)
		return writeErr
	}

	if err := os.Rename(temporaryFilePath, repository.recordFilePath); err != nil {
		_ = os.Remove(temporaryFilePath)
		return fmt.Errorf("replacing run record file: %w", err)
	}

	return nil
}

// compactIfNeeded 在某個 job 的紀錄超出保留筆數時，只丟掉該 job 最舊的幾筆。
//
// 逐 job 而非全域上限：一個跑得很勤的 job 不該把另一個一天只跑一次的 job 的歷史
// 擠掉。
func (repository *JobRunRepository) compactIfNeeded(jobID string) error {
	if repository.retentionCount <= 0 {
		return nil
	}

	records, err := repository.readRecords()
	if err != nil {
		return err
	}

	matchingRecords := filterRecordsByJobID(records, jobID)
	if len(matchingRecords) <= repository.retentionCount {
		return nil
	}

	sortRecordsNewestFirst(matchingRecords)
	retainedRunIDs := make(map[string]bool, repository.retentionCount)
	for _, record := range matchingRecords[:repository.retentionCount] {
		retainedRunIDs[record.RunID] = true
	}

	retainedRecords := make([]jobRunRecord, 0, len(records))
	for _, record := range records {
		if record.JobID == jobID && !retainedRunIDs[record.RunID] {
			continue
		}
		retainedRecords = append(retainedRecords, record)
	}

	return repository.rewriteRecords(retainedRecords)
}

func filterRecordsByJobID(records []jobRunRecord, jobID string) []jobRunRecord {
	matchingRecords := make([]jobRunRecord, 0, len(records))
	for _, record := range records {
		if record.JobID == jobID {
			matchingRecords = append(matchingRecords, record)
		}
	}

	return matchingRecords
}

// sortRecordsNewestFirst 依開始時刻新到舊排序。相同時刻以 RunID 決勝，讓順序
// 穩定可測。
func sortRecordsNewestFirst(records []jobRunRecord) {
	sort.SliceStable(records, func(leftIndex int, rightIndex int) bool {
		if records[leftIndex].StartedAt.Equal(records[rightIndex].StartedAt) {
			return records[leftIndex].RunID > records[rightIndex].RunID
		}

		return records[leftIndex].StartedAt.After(records[rightIndex].StartedAt)
	})
}

func buildRecord(run *entity.JobRun) jobRunRecord {
	record := jobRunRecord{
		RunID:           run.RunID(),
		JobID:           run.JobID(),
		TriggerSource:   string(run.TriggerSource()),
		RunStatus:       string(run.RunStatus()),
		StartedAt:       run.StartedAt(),
		OutputExcerpt:   run.OutputExcerpt(),
		OutputTruncated: run.OutputTruncated(),
	}

	if finishedAt, finished := run.FinishedAt(); finished {
		record.FinishedAt = &finishedAt
	}

	if exitCode, known := run.ExitCode(); known {
		record.ExitCode = &exitCode
	}

	return record
}

func restoreRun(record jobRunRecord) *entity.JobRun {
	finishedAt := time.Time{}
	if record.FinishedAt != nil {
		finishedAt = *record.FinishedAt
	}

	exitCode := 0
	exitCodeKnown := false
	if record.ExitCode != nil {
		exitCode = *record.ExitCode
		exitCodeKnown = true
	}

	return entity.RestoreJobRun(
		record.RunID,
		record.JobID,
		record.TriggerSource,
		record.RunStatus,
		record.StartedAt,
		finishedAt,
		exitCode,
		exitCodeKnown,
		record.OutputExcerpt,
		record.OutputTruncated,
	)
}
