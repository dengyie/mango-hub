package rpc

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// maxRPCBatchSize caps JSON-RPC batch arrays; each entry is dispatched
// serially, so an unbounded batch is a CPU amplification vector.
const maxRPCBatchSize = 32

// ParseRequest 解析单个 JSON-RPC 请求。返回请求与错误（解析层面）。
func ParseRequest(data []byte) (*JsonRpcRequest, *JsonRpcError) {
	requests, err := ParseRequests(data)
	if err != nil {
		return nil, err
	}
	if len(requests) == 0 {
		return nil, &JsonRpcError{Code: InvalidRequest, Message: "no requests found"}
	}
	return requests[0], nil
}

// ParseRequests 解析单个或批量 JSON-RPC 请求。返回请求切片与错误（解析层面），
// 若是批量空数组则返回 InvalidRequest 错误（协议要求）。
func ParseRequests(data []byte) ([]*JsonRpcRequest, *JsonRpcError) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, &JsonRpcError{Code: ParseError, Message: "empty body"}
	}
	first := data[0]
	if first == '{' { // 单个
		var r JsonRpcRequest
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, &JsonRpcError{Code: ParseError, Message: "invalid json", Data: err.Error()}
		}
		if e := r.Validate(); e != nil {
			return nil, e
		}
		return []*JsonRpcRequest{&r}, nil
	}
	if first == '[' { // 批量
		var arr []JsonRpcRequest
		if err := json.Unmarshal(data, &arr); err != nil {
			return nil, &JsonRpcError{Code: ParseError, Message: "invalid json", Data: err.Error()}
		}
		if len(arr) == 0 {
			return nil, &JsonRpcError{Code: InvalidRequest, Message: "empty batch"}
		}
		// Cap the batch: every entry runs serially through dispatch, so an
		// unbounded anonymous batch array is a CPU amplification vector.
		if len(arr) > maxRPCBatchSize {
			return nil, &JsonRpcError{Code: InvalidRequest, Message: "batch too large", Data: fmt.Sprintf("max %d requests per batch", maxRPCBatchSize)}
		}
		res := make([]*JsonRpcRequest, 0, len(arr))
		for i := range arr {
			rr := arr[i]
			if e := rr.Validate(); e != nil {
				return nil, e
			}
			res = append(res, &rr)
		}
		return res, nil
	}
	return nil, &JsonRpcError{Code: ParseError, Message: "invalid json: not object/array"}
}
