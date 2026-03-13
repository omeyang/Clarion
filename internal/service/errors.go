package service

import "errors"

// ErrNotFound 表示请求的资源不存在。
var ErrNotFound = errors.New("资源不存在")
