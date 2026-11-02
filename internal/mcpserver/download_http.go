package mcpserver

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cmstar/jumpaccess/internal/application/downloadcache"
)

type downloadBaseKey struct{}

func downloadBaseURL(ctx context.Context) string {
	value, _ := ctx.Value(downloadBaseKey{}).(string)
	return value
}

type downloadHTTPTimeouts struct {
	Idle, Total time.Duration
}

func downloadHandler(cache *downloadcache.Store, limits downloadHTTPTimeouts) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "文件领取仅支持 GET 或 HEAD", http.StatusMethodNotAllowed)
			return
		}
		if r.ContentLength > 0 || len(r.TransferEncoding) != 0 {
			// 正文领取不读取请求体；避免清除读截止时间后 HTTP 服务等待其余请求体。
			w.Header().Set("Connection", "close")
			http.Error(w, "文件领取请求不能包含正文", http.StatusBadRequest)
			return
		}
		if cache == nil {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, downloadcache.RoutePrefix)
		query, err := parseDownloadQuery(r)
		if err != nil || name == r.URL.Path || strings.ContainsAny(name, "/\\") {
			http.Error(w, "文件领取地址无效", http.StatusForbidden)
			return
		}
		lease, err := cache.Open(name, query[0], query[1], r.Method == http.MethodHead)
		if err != nil {
			status, message := http.StatusInternalServerError, "文件暂时无法领取"
			switch {
			case errors.Is(err, downloadcache.ErrBusy):
				status, message = http.StatusTooManyRequests, "文件领取繁忙，请稍后重试同一链接"
				w.Header().Set("Retry-After", "5")
			case errors.Is(err, downloadcache.ErrExpired):
				status, message = http.StatusGone, "文件领取链接已过期"
			case errors.Is(err, downloadcache.ErrNotFound):
				status, message = http.StatusNotFound, "文件已不可领取"
			case errors.Is(err, downloadcache.ErrInvalidToken), errors.Is(err, downloadcache.ErrInvalidName):
				status, message = http.StatusForbidden, "文件领取凭据无效"
			case errors.Is(err, downloadcache.ErrClosed):
				status, message = http.StatusServiceUnavailable, "文件领取服务已停止"
			}
			http.Error(w, message, status)
			return
		}
		defer lease.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.FormatInt(lease.Size, 10))
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": lease.DownloadName}))
		if r.Method == http.MethodHead {
			return
		}
		streamDownload(w, r, lease.File, cache.Done(), limits)
	})
}

func parseDownloadQuery(r *http.Request) ([2]string, error) {
	var values [2]string
	query, err := url.ParseQuery(r.URL.RawQuery)
	// 只接受一份受签名保护的保存名及签名，避免歧义或意外带入路径。
	if err != nil || len(query) != 2 || len(query["filename"]) != 1 || len(query["signature"]) != 1 {
		err = errors.New("invalid download query")
	} else {
		values = [2]string{query.Get("filename"), query.Get("signature")}
	}
	return values, err
}

func streamDownload(w http.ResponseWriter, r *http.Request, source io.Reader, stopped <-chan struct{}, limits downloadHTTPTimeouts) {
	ctx, cancel := context.WithTimeout(r.Context(), limits.Total)
	defer cancel()
	controller := http.NewResponseController(w)
	// HTTP 服务的默认读写截止时间用于 MCP；领取正文采用独立的进展和总时限。
	if err := controller.SetReadDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return
	}
	closed := make(chan struct{})
	defer close(closed)
	go func() {
		select {
		case <-stopped:
			cancel()
		case <-closed:
		}
	}()
	interrupted := make(chan struct{})
	var deadlineMu sync.Mutex
	stopInterrupt := context.AfterFunc(ctx, func() {
		deadlineMu.Lock()
		_ = controller.SetWriteDeadline(time.Now())
		deadlineMu.Unlock()
		close(interrupted)
	})
	defer func() {
		if !stopInterrupt() {
			<-interrupted
		}
	}()
	end, _ := ctx.Deadline()
	advanceDeadline := func() error {
		// 取消与正常续期互斥：Read 返回得再晚，也不能覆盖取消设置的立即截止时间。
		deadlineMu.Lock()
		defer deadlineMu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		deadline := time.Now().Add(limits.Idle)
		if end.Before(deadline) {
			deadline = end
		}
		err := controller.SetWriteDeadline(deadline)
		if errors.Is(err, http.ErrNotSupported) {
			return nil
		}
		return err
	}
	buffer := make([]byte, 32<<10)
	for ctx.Err() == nil {
		n, readErr := source.Read(buffer)
		if n > 0 {
			if err := advanceDeadline(); err != nil {
				return
			}
			written, err := w.Write(buffer[:n])
			if err != nil || written != n {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		}
		if readErr != nil {
			return
		}
	}
}
