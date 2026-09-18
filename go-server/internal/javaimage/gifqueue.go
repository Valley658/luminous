package javaimage

import (
	"os"
	"path/filepath"
	"strings"
)

type gifJob struct {
	client  *Client
	path    string
	quality int
	onDone  func(newPath string, ok bool)
}

const gifQueueWorkers = 3

var gifQueue = make(chan gifJob, 200)

func init() {
	for i := 0; i < gifQueueWorkers; i++ {
		go gifQueueWorker()
	}
}

func gifQueueWorker() {
	for job := range gifQueue {
		webpPath := strings.TrimSuffix(job.path, filepath.Ext(job.path)) + ".webp"
		_, ok := job.client.Call("/gif-to-webp", map[string]any{
			"path": absPath(job.path), "outputPath": absPath(webpPath), "quality": job.quality,
		})
		if ok {
			_ = os.Remove(job.path)
		}
		if job.onDone == nil {
			continue
		}
		if ok {
			job.onDone(webpPath, true)
		} else {
			job.onDone("", false)
		}
	}
}

func (c *Client) QueueGifToWebp(filePath string, quality int, onDone func(newPath string, ok bool)) bool {
	select {
	case gifQueue <- gifJob{client: c, path: filePath, quality: quality, onDone: onDone}:
		return true
	default:
		return false
	}
}

func (c *Client) runGifJobSync(filePath string, quality int) (webpPath string, ok bool) {
	type result struct {
		path string
		ok   bool
	}
	done := make(chan result, 1)
	enqueued := c.QueueGifToWebp(filePath, quality, func(newPath string, ok bool) {
		done <- result{newPath, ok}
	})
	if !enqueued {
		return "", false
	}
	r := <-done
	return r.path, r.ok
}
