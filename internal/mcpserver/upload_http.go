package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cmstar/jumpaccess/internal/application/uploadcache"
)

func uploadHandler(cache *uploadcache.Store, limits downloadHTTPTimeouts) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		fail := func(status int, message string) {
			w.Header().Set("Connection", "close")
			http.Error(w, message, status)
		}
		if r.Method != http.MethodPut {
			w.Header().Set("Allow", "PUT")
			fail(405, "上传内容仅支持 PUT")
			return
		}
		if cache == nil {
			fail(404, "上传接收未启用")
			return
		}
		id := strings.TrimPrefix(r.URL.Path, uploadcache.RoutePrefix)
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil || len(q) != 1 || len(q["signature"]) != 1 || id == r.URL.Path || strings.ContainsAny(id, "/\\") {
			fail(403, "上传接收地址无效")
			return
		}
		if r.Header.Get("Content-Encoding") != "" || r.Header.Get("Content-Range") != "" {
			fail(400, "上传须发送完整准备包，不支持编码转换或分段续传")
			return
		}
		receiver, err := cache.BeginReceive(id, q.Get("signature"))
		if err != nil {
			status, message := 500, "上传内容暂时无法接收"
			switch {
			case errors.Is(err, uploadcache.ErrInvalidToken):
				status, message = 403, "上传接收凭据无效"
			case errors.Is(err, uploadcache.ErrExpired):
				status, message = 410, "上传接收资格已过期或不存在"
			case errors.Is(err, uploadcache.ErrBusy):
				status, message = 429, "HTTP 接收繁忙，请稍后重试同一地址"
				w.Header().Set("Retry-After", "5")
			case errors.Is(err, uploadcache.ErrUsed):
				status, message = 409, "内容已固定或已提交，不可再次写入"
			case errors.Is(err, uploadcache.ErrClosed):
				status, message = 503, "上传接收已停止"
			}
			fail(status, message)
			return
		}
		// 无论正文短收、超收或中断，返回前都关闭文件并清理半成品。
		defer receiver.Abort()
		if r.ContentLength >= 0 && r.ContentLength != receiver.Size() {
			fail(400, "请求正文长度与声明的准备包大小不同")
			return
		}
		if err = receiveUpload(w, r, receiver, cache.Done(), limits); err != nil {
			receiver.Abort()
			status, message := 400, "上传内容未完整接收，请检查文件后重试同一接收对象"
			var timeout interface{ Timeout() bool }
			var disk *os.PathError
			if errors.As(err, &disk) {
				status, message = 500, "上传缓存写入失败，请检查服务电脑存储空间和权限"
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, uploadcache.ErrClosed) || errors.As(err, &timeout) && timeout.Timeout() {
				status, message = 408, "上传接收已取消或超时"
			}
			fail(status, message)
			return
		}
		view, err := cache.Get(id)
		if err != nil {
			fail(410, "上传接收对象已失效")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(view)
	})
}

func receiveUpload(w http.ResponseWriter, r *http.Request, receiver *uploadcache.Receiver, stopped <-chan struct{}, limits downloadHTTPTimeouts) error {
	ctx, cancel := context.WithTimeout(r.Context(), limits.Total)
	defer cancel()
	controller := http.NewResponseController(w)
	end, _ := ctx.Deadline()
	if err := controller.SetWriteDeadline(end.Add(5 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-stopped:
			cancel()
		case <-receiver.Done():
			cancel()
		case <-finished:
		}
	}()
	var deadlineMu sync.Mutex
	interrupted := make(chan struct{})
	stopInterrupt := context.AfterFunc(ctx, func() {
		deadlineMu.Lock()
		_ = controller.SetReadDeadline(time.Now())
		deadlineMu.Unlock()
		_ = r.Body.Close()
		close(interrupted)
	})
	defer func() {
		if !stopInterrupt() {
			<-interrupted
		}
	}()
	advance := func() error {
		deadlineMu.Lock()
		defer deadlineMu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		deadline := time.Now().Add(limits.Idle)
		if end.Before(deadline) {
			deadline = end
		}
		err := controller.SetReadDeadline(deadline)
		if errors.Is(err, http.ErrNotSupported) {
			return nil
		}
		return err
	}
	buffer := make([]byte, 32<<10)
	for {
		if err := advance(); err != nil {
			return err
		}
		n, readErr := r.Body.Read(buffer)
		if n > 0 {
			written, err := receiver.Write(buffer[:n])
			if err != nil {
				return err
			}
			if written != n {
				return io.ErrShortWrite
			}
		}
		if readErr != nil {
			if err := ctx.Err(); err != nil {
				return err
			}
			if readErr != io.EOF {
				return readErr
			}
			return receiver.Complete()
		}
	}
}
