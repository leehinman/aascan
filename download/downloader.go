package download

import (
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Progress struct {
	Downloaded int64
	Total      int64
	Done       bool
	Path       string
	Err        error
}

var httpClient = &http.Client{}

func Download(url, shaURL string, ch chan<- Progress) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		ch <- Progress{Err: err}
		return
	}

	filename := filepath.Base(strings.SplitN(url, "?", 2)[0])
	dest := filepath.Join(homeDir, "Downloads", filename)
	tmp := dest + ".download"

	resp, err := httpClient.Get(url)
	if err != nil {
		ch <- Progress{Err: err}
		return
	}
	defer resp.Body.Close()

	total := resp.ContentLength

	f, err := os.Create(tmp)
	if err != nil {
		ch <- Progress{Err: err}
		return
	}

	hasher := sha512.New()
	writer := io.MultiWriter(f, hasher)

	buf := make([]byte, 32*1024)
	var downloaded int64
	lastReport := time.Now()

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := writer.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(tmp)
				ch <- Progress{Err: werr}
				return
			}
			downloaded += int64(n)
			if time.Since(lastReport) > 80*time.Millisecond {
				ch <- Progress{Downloaded: downloaded, Total: total}
				lastReport = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			f.Close()
			os.Remove(tmp)
			ch <- Progress{Err: readErr}
			return
		}
	}
	f.Close()

	if shaURL != "" {
		if err := verifySHA512(tmp, shaURL, hex.EncodeToString(hasher.Sum(nil))); err != nil {
			os.Remove(tmp)
			ch <- Progress{Err: err}
			return
		}
	}

	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		ch <- Progress{Err: err}
		return
	}

	ch <- Progress{Downloaded: downloaded, Total: downloaded, Done: true, Path: dest}
}

func verifySHA512(localPath, shaURL, localHash string) error {
	resp, err := httpClient.Get(shaURL)
	if err != nil {
		return nil // skip verification if sha file unavailable
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	remoteHash := strings.Fields(string(data))[0]
	if !strings.EqualFold(localHash, remoteHash) {
		return fmt.Errorf("sha512 mismatch: expected %s, got %s", remoteHash, localHash)
	}
	return nil
}
