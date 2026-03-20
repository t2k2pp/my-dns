// Package logger provides a rotating, ZIP-compressed CSV query logger.
// Log columns: Timestamp, ClientIP, Domain, QueryType, Action, Latency_ms
package logger

import (
	"archive/zip"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CSVHeader is the header row written to every new log file.
var CSVHeader = []string{"Timestamp", "ClientIP", "Domain", "QueryType", "Action", "Latency_ms"}

// QueryLogger writes DNS query records to a size-rotating CSV log.
// When the file reaches maxBytes it is renamed, a fresh file is opened,
// and the old data is compressed to a ZIP archive asynchronously.
// All public methods are safe for concurrent use.
type QueryLogger struct {
	mu       sync.Mutex
	filePath string
	maxBytes int64
	file     *os.File
	writer   *csv.Writer
	size     int64
}

// New opens (or creates) the log file at filePath and returns a QueryLogger.
func New(filePath string, maxBytes int64) (*QueryLogger, error) {
	ql := &QueryLogger{filePath: filePath, maxBytes: maxBytes}
	if err := ql.openFile(); err != nil {
		return nil, err
	}
	return ql, nil
}

// Write appends a single query record. Thread-safe.
func (ql *QueryLogger) Write(clientIP, domain, queryType, action string, latencyMs int64) {
	ql.mu.Lock()
	defer ql.mu.Unlock()

	record := []string{
		time.Now().UTC().Format(time.RFC3339),
		clientIP,
		domain,
		queryType,
		action,
		fmt.Sprintf("%d", latencyMs),
	}
	_ = ql.writer.Write(record)
	ql.writer.Flush()

	// Approximate byte cost; exact tracking would require syscall per write.
	ql.size += int64(len(strings.Join(record, ",")) + 2)

	if ql.size >= ql.maxBytes {
		ql.rotate()
	}
}

// Close flushes buffered data and closes the underlying file.
func (ql *QueryLogger) Close() {
	ql.mu.Lock()
	defer ql.mu.Unlock()
	if ql.writer != nil {
		ql.writer.Flush()
	}
	if ql.file != nil {
		_ = ql.file.Close()
	}
}

// openFile opens or creates the log file and initialises the CSV writer.
// Caller must hold ql.mu when this is called during rotation; safe to call
// unlocked during initialisation.
func (ql *QueryLogger) openFile() error {
	f, err := os.OpenFile(ql.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log file %q: %w", ql.filePath, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	ql.file = f
	ql.size = info.Size()
	ql.writer = csv.NewWriter(f)

	if ql.size == 0 {
		_ = ql.writer.Write(CSVHeader)
		ql.writer.Flush()
		ql.size = int64(len(strings.Join(CSVHeader, ",")) + 2)
	}
	return nil
}

// rotate closes the current log file, opens a fresh one, and compresses
// the old data in a background goroutine.
// Caller must hold ql.mu.
func (ql *QueryLogger) rotate() {
	ql.writer.Flush()
	_ = ql.file.Close()
	ql.file = nil
	ql.writer = nil

	tmp := ql.filePath + ".rotating"
	if err := os.Rename(ql.filePath, tmp); err != nil {
		fmt.Fprintf(os.Stderr, "[logger] rotate rename: %v\n", err)
		_ = ql.openFile()
		return
	}

	if err := ql.openFile(); err != nil {
		fmt.Fprintf(os.Stderr, "[logger] rotate reopen: %v\n", err)
		return
	}

	go compressAndRemove(tmp, ql.filePath)
}

// compressAndRemove zips src into a timestamped archive in the same directory
// as basePath, then removes src.
func compressAndRemove(src, basePath string) {
	dir := filepath.Dir(basePath)
	base := strings.TrimSuffix(filepath.Base(basePath), filepath.Ext(basePath))
	ts := time.Now().UTC().Format("20060102_150405")
	zipPath := filepath.Join(dir, fmt.Sprintf("%s_%s.zip", base, ts))

	if err := createZip(src, zipPath, filepath.Base(basePath)); err != nil {
		fmt.Fprintf(os.Stderr, "[logger] compress: %v\n", err)
		return
	}
	_ = os.Remove(src)
	fmt.Printf("[logger] rotated → %s\n", zipPath)
}

// createZip writes src as nameInZip inside a new ZIP archive at dst.
func createZip(src, dst, nameInZip string) error {
	zf, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create zip %q: %w", dst, err)
	}
	defer zf.Close()

	zw := zip.NewWriter(zf)
	defer zw.Close()

	sf, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source %q: %w", src, err)
	}
	defer sf.Close()

	w, err := zw.Create(nameInZip)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, sf)
	return err
}
