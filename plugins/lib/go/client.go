// Package plugins 提供插件 SDK，用于插件与 Agent 之间的通信
// 插件通过 Pipe（文件描述符 3/4）与 Agent 进行双向通信
package plugins

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/imkerbos/mxsec-platform/api/proto/bridge"
	"google.golang.org/protobuf/proto"
)

// Client 是插件客户端，封装了与 Agent 的 Pipe 通信
type Client struct {
	rx     io.ReadCloser  // 接收管道（从 Agent 读取）
	tx     io.WriteCloser // 发送管道（向 Agent 写入）
	reader *bufio.Reader  // 带缓冲的读取器
	writer *bufio.Writer  // 带缓冲的写入器
	rmu    *sync.Mutex    // 读取锁
	wmu    *sync.Mutex    // 写入锁
}

// NewClient 创建新的插件客户端
// 插件通过文件描述符 3（rx）和 4（tx）与 Agent 通信
func NewClient() (*Client, error) {
	// 文件描述符 3：Agent 写入，插件读取（接收任务）
	rx := os.NewFile(3, "rx")
	if rx == nil {
		return nil, fmt.Errorf("failed to open file descriptor 3 (rx)")
	}

	// 文件描述符 4：插件写入，Agent 读取（发送数据）
	tx := os.NewFile(4, "tx")
	if tx == nil {
		return nil, fmt.Errorf("failed to open file descriptor 4 (tx)")
	}

	return &Client{
		rx:     rx,
		tx:     tx,
		reader: bufio.NewReader(rx),
		writer: bufio.NewWriter(tx),
		rmu:    &sync.Mutex{},
		wmu:    &sync.Mutex{},
	}, nil
}

// SendRecord 发送记录到 Agent
// 协议格式：4 字节长度（小端序） + protobuf 序列化的 Record
func (c *Client) SendRecord(rec *bridge.Record) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()

	// 序列化 Record
	buf, err := proto.Marshal(rec)
	if err != nil {
		return fmt.Errorf("failed to marshal record: %w", err)
	}

	// 写入长度（4 字节，小端序）
	size := uint32(len(buf))
	if err := binary.Write(c.writer, binary.LittleEndian, size); err != nil {
		return fmt.Errorf("failed to write record size: %w", err)
	}

	// 写入数据
	if _, err := c.writer.Write(buf); err != nil {
		return fmt.Errorf("failed to write record data: %w", err)
	}

	// 立即刷新缓冲区，确保数据及时发送
	if err := c.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	return nil
}

// SendRecordWithRetry 发送记录到 Agent，带重试机制
func (c *Client) SendRecordWithRetry(rec *bridge.Record, maxRetries int, retryDelay time.Duration) error {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if err := c.SendRecord(rec); err == nil {
			return nil
		} else {
			lastErr = err
			if i < maxRetries-1 {
				time.Sleep(retryDelay)
			}
		}
	}
	return fmt.Errorf("failed to send record after %d retries: %w", maxRetries, lastErr)
}

// ReceiveTask 从 Agent 接收任务
// 协议格式：4 字节长度（小端序） + protobuf 序列化的 Task
func (c *Client) ReceiveTask() (*bridge.Task, error) {
	c.rmu.Lock()
	defer c.rmu.Unlock()

	// 读取长度（4 字节，小端序）
	var len uint32
	if err := binary.Read(c.reader, binary.LittleEndian, &len); err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("failed to read task size: %w", err)
	}

	// 限制最大消息大小（防止恶意数据）
	const maxMessageSize = 10 * 1024 * 1024 // 10MB
	if len > maxMessageSize {
		return nil, fmt.Errorf("task size %d exceeds maximum %d", len, maxMessageSize)
	}

	// 读取数据
	buf := make([]byte, len)
	if _, err := io.ReadFull(c.reader, buf); err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("failed to read task data: %w", err)
	}

	// 反序列化 Task
	task := &bridge.Task{}
	if err := proto.Unmarshal(buf, task); err != nil {
		return nil, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	return task, nil
}

// ReceiveTaskWithTimeout 从 Agent 接收任务，带超时机制
func (c *Client) ReceiveTaskWithTimeout(timeout time.Duration) (*bridge.Task, error) {
	type result struct {
		task *bridge.Task
		err  error
	}

	ch := make(chan result, 1)
	go func() {
		task, err := c.ReceiveTask()
		ch <- result{task: task, err: err}
	}()

	select {
	case res := <-ch:
		return res.task, res.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("receive task timeout after %v", timeout)
	}
}

// Flush 刷新写入缓冲区
func (c *Client) Flush() error {
	c.wmu.Lock()
	defer c.wmu.Unlock()

	if c.writer.Buffered() != 0 {
		if err := c.writer.Flush(); err != nil {
			return fmt.Errorf("failed to flush writer: %w", err)
		}
	}
	return nil
}

// Close 关闭客户端，释放资源
func (c *Client) Close() error {
	var errs []error

	// 刷新缓冲区
	if err := c.Flush(); err != nil {
		errs = append(errs, fmt.Errorf("flush error: %w", err))
	}

	// 关闭管道
	if err := c.rx.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close rx error: %w", err))
	}

	if err := c.tx.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close tx error: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors during close: %v", errs)
	}

	return nil
}
