package agentmcp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
)

type cursor struct {
	Instance string
	Query    string
	Offset   int
}

func (q *Queries) page(input PageInput, filter ...string) (offset, limit int, key string, err error) {
	limit = input.Limit
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 200 {
		return 0, 0, "", invalid("分页大小必须在 1–200 之间")
	}
	encoded, _ := json.Marshal(filter)
	digest := sha256.Sum256(encoded)
	key = base64.RawURLEncoding.EncodeToString(digest[:])
	if input.Cursor == "" {
		return 0, limit, key, nil
	}
	bad := &QueryError{Code: "INVALID_CURSOR", Message: "分页游标无效、已过期或不属于当前查询"}
	if len(input.Cursor) > 2048 {
		return 0, 0, "", bad
	}
	parts := strings.Split(input.Cursor, ".")
	if len(parts) != 2 {
		return 0, 0, "", bad
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, 0, "", bad
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, 0, "", bad
	}
	mac := hmac.New(sha256.New, q.cursorKey)
	mac.Write(data)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return 0, 0, "", bad
	}
	var value cursor
	if json.Unmarshal(data, &value) != nil || value.Instance != q.instance || value.Query != key || value.Offset < 0 || value.Offset > 1<<30 {
		return 0, 0, "", bad
	}
	return value.Offset, limit, key, nil
}

func (q *Queries) nextCursor(offset int, key string) string {
	data, _ := json.Marshal(cursor{Instance: q.instance, Query: key, Offset: offset})
	mac := hmac.New(sha256.New, q.cursorKey)
	mac.Write(data)
	return base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func pageResult[T any](q *Queries, items []T, total, offset int, key, profile, org string) Page[T] {
	if items == nil {
		items = make([]T, 0)
	}
	page := Page[T]{Profile: profile, Organization: org, Items: items, Total: total}
	if len(items) > 0 && offset+len(items) < total {
		page.NextCursor = q.nextCursor(offset+len(items), key)
	}
	return page
}

func window[T any](items []T, offset, limit int) []T {
	if offset >= len(items) {
		return nil
	}
	return items[offset:min(offset+limit, len(items))]
}
