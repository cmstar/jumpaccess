package agentmcp

import (
	"container/list"
	"crypto/sha256"
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Fault 只携带适合向 Agent 返回的信息，不透传凭据或上游原始响应。
type Fault struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

func (f *Fault) Error() string          { return f.Code + ": " + f.Message }
func fault(code, message string) *Fault { return &Fault{Code: code, Message: message} }

type requestRecord struct {
	ResourceID string
	Failure    *Fault
	done       chan struct{}
	key        string
	epoch      uint64
	digest     [32]byte
	terminal   bool
}

// requests 不保留命令、环境或输出。调用方必须保证 admit 不阻塞、不递归调用登记器。
// 如需同时占用连接，调用方先取得业务锁，再调用 begin/finish；锁顺序不可反转。
type requests struct {
	mu                                sync.Mutex
	instance                          string
	current                           uint64
	historyLimit, activeLimit, active int
	records                           map[string]*requestRecord
	resources                         map[string]int
	fifo                              list.List
}

func newRequests(instance string, historyLimit, activeLimit int) *requests {
	if historyLimit < 1 || activeLimit < 1 {
		panic("request limits must be positive")
	}
	return &requests{instance: instance, current: 1, historyLimit: historyLimit, activeLimit: activeLimit, records: make(map[string]*requestRecord), resources: make(map[string]int)}
}

func (r *requests) epoch() uint64 { r.mu.Lock(); defer r.mu.Unlock(); return r.current }

var requestUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (r *requests) begin(tool, key string, params any, admit func() (string, error)) (*requestRecord, bool, error) {
	parts := strings.Split(key, ":")
	if len(parts) != 3 || !requestUUID.MatchString(parts[2]) {
		return nil, false, fault("INVALID_REQUEST_KEY", "请求键格式必须为实例:代号:UUID")
	}
	if parts[0] != r.instance {
		return nil, false, fault("INSTANCE_CHANGED", "服务实例已改变，旧请求不能重放")
	}
	epoch, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || epoch == 0 || strconv.FormatUint(epoch, 10) != parts[1] {
		return nil, false, fault("INVALID_REQUEST_KEY", "请求代号无效")
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, false, fault("INVALID_ARGUMENT", "请求参数无法登记")
	}
	digest := sha256.Sum256(encoded)
	clear(encoded)
	index := tool + ":" + key
	r.mu.Lock()
	defer r.mu.Unlock()
	if old := r.records[index]; old != nil {
		if old.digest != digest {
			return nil, false, fault("IDEMPOTENCY_CONFLICT", "相同请求键的业务参数不同")
		}
		return old, false, nil
	}
	if epoch < r.current {
		return nil, false, fault("REQUEST_EXPIRED", "请求记录已过期；不得自动换键重放结果不明的命令")
	}
	if epoch != r.current {
		return nil, false, fault("INVALID_REQUEST_KEY", "请使用当前请求代号")
	}
	if r.active >= r.activeLimit {
		return nil, false, fault("LIMIT_EXCEEDED", "活动请求已满")
	}
	id, err := admit()
	if err != nil {
		return nil, false, err
	}
	record := &requestRecord{ResourceID: id, key: index, epoch: epoch, digest: digest, done: make(chan struct{})}
	r.records[index] = record
	r.resources[id]++
	r.active++
	return record, true, nil
}

func (r *requests) finish(record *requestRecord, failure *Fault) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if record.terminal {
		return
	}
	record.terminal, record.Failure = true, failure
	r.active--
	if r.fifo.Len() >= r.historyLimit {
		first := r.fifo.Front()
		old := first.Value.(*requestRecord)
		if old.epoch == r.current {
			// 实例生存期内不允许代号回绕。达到极限时禁止新登记。
			if r.current == math.MaxUint64 {
				panic("request epoch exhausted")
			}
			r.current++
		}
		delete(r.records, old.key)
		r.resources[old.ResourceID]--
		if r.resources[old.ResourceID] == 0 {
			delete(r.resources, old.ResourceID)
		}
		r.fifo.Remove(first)
	}
	r.fifo.PushBack(record)
	close(record.done)
}

func (r *requests) knows(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.resources[id] > 0
}
