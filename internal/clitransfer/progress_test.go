package clitransfer

import (
	"bytes"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestProgressCompletionStatistics(t *testing.T) {
	for _, upload := range []bool{false, true} {
		direction, pending := "download", "Saving"
		if upload {
			direction, pending = "upload", "Waiting for confirmation"
		}
		t.Run(direction, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var output bytes.Buffer
				p := newProgress(&output, upload)
				// 本地选择时间不计入；最终保存或确认时间计入。
				time.Sleep(time.Minute)
				p.update("file.bin", 1024*1024, 0, false)
				time.Sleep(1500 * time.Millisecond)
				p.update("file.bin", 1024*1024, 1024*1024, false)
				if !strings.Contains(output.String(), pending) || strings.Contains(output.String(), "Elapsed") || strings.Contains(output.String(), "100%") {
					t.Fatalf("尚未确认时的进度 = %q", output.String())
				}
				time.Sleep(500 * time.Millisecond)
				p.update("file.bin", 1024*1024, 1024*1024, true)
				want := "\r\x1b[2K100% - 1.00 / 1.00 MiB - Complete - Elapsed 2.00 s - Avg 512.00 KiB/s\r\n"
				if !strings.HasSuffix(output.String(), want) {
					t.Fatalf("完成统计 = %q，期望后缀 %q", output.String(), want)
				}
			})
		})
	}
}

func TestProgressStatisticsResetForEachFile(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var output bytes.Buffer
		p := newProgress(&output, false)
		p.update("file.bin", 1024, 0, false)
		time.Sleep(2 * time.Second)
		p.update("file.bin", 1024, 1024, true)
		if !strings.Contains(output.String(), "Elapsed 2.00 s - Avg 512 B/s") {
			t.Fatalf("首个文件统计 = %q", output.String())
		}
		time.Sleep(time.Minute)
		// 同名的下一文件也重新计时。
		p.update("file.bin", 2048, 0, false)
		time.Sleep(500 * time.Millisecond)
		p.update("file.bin", 2048, 2048, true)
		if !strings.HasSuffix(output.String(), "Elapsed 0.50 s - Avg 4.00 KiB/s\r\n") {
			t.Fatalf("下个文件统计 = %q", output.String())
		}
	})
}

func TestProgressResumeExcludesExistingBytes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var output bytes.Buffer
		p := newProgress(&output, true)
		p.update("file.bin", 1024*1024, 512*1024, false)
		time.Sleep(2 * time.Second)
		p.update("file.bin", 1024*1024, 1024*1024, true)
		if !strings.HasSuffix(output.String(), "Elapsed 2.00 s - Avg 256.00 KiB/s\r\n") {
			t.Fatalf("续传统计包含了远端已有内容：%q", output.String())
		}
	})
}

func TestProgressUploadIncludesOfferConfirmation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var output bytes.Buffer
		p := newProgress(&output, true)
		time.Sleep(time.Minute)
		p.start("file.bin")
		// 与控制器一样，先计时，再等待远端接受文件并给出续传偏移。
		time.Sleep(1500 * time.Millisecond)
		p.update("file.bin", 1024*1024, 512*1024, false)
		time.Sleep(500 * time.Millisecond)
		p.update("file.bin", 1024*1024, 1024*1024, true)
		if !strings.HasSuffix(output.String(), "Elapsed 2.00 s - Avg 256.00 KiB/s\r\n") {
			t.Fatalf("上传统计未包含文件协商时间：%q", output.String())
		}
	})
}

func TestProgressStatisticsDurationAndSpeed(t *testing.T) {
	for _, test := range []struct {
		size    int64
		elapsed time.Duration
		want    string
	}{
		{0, 0, "Elapsed <0.01 s - Avg 0 B/s"},
		{1024, 0, "Elapsed <0.01 s - Avg —"},
		{1, time.Millisecond, "Elapsed <0.01 s - Avg 1000 B/s"},
		{1024, 10 * time.Millisecond, "Elapsed 0.01 s - Avg 100.00 KiB/s"},
		{1024 * 1024, time.Second, "Elapsed 1.00 s - Avg 1.00 MiB/s"},
		{1024 * 1024 * 1024, time.Second, "Elapsed 1.00 s - Avg 1.00 GiB/s"},
		{0, 59999 * time.Millisecond, "Elapsed 1 m 0.00 s - Avg 0 B/s"},
		{0, 62500 * time.Millisecond, "Elapsed 1 m 2.50 s - Avg 0 B/s"},
		{0, 3662500 * time.Millisecond, "Elapsed 1 h 1 m 2.50 s - Avg 0 B/s"},
	} {
		t.Run(test.want, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var output bytes.Buffer
				p := newProgress(&output, false)
				p.update("file.bin", test.size, 0, false)
				time.Sleep(test.elapsed)
				p.update("file.bin", test.size, test.size, true)
				if !strings.HasSuffix(output.String(), test.want+"\r\n") {
					t.Fatalf("完成统计 = %q，期望后缀 %q", output.String(), test.want)
				}
			})
		})
	}
}
