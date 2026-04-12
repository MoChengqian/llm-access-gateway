package redis

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Address     string
	Password    string
	DB          int
	DialTimeout time.Duration
}

type Client struct {
	address     string
	password    string
	db          int
	dialTimeout time.Duration
}

type respReader struct {
	reader *bufio.Reader
}

func NewClient(cfg Config) Client {
	timeout := cfg.DialTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	return Client{
		address:     cfg.Address,
		password:    cfg.Password,
		db:          cfg.DB,
		dialTimeout: timeout,
	}
}

func (c Client) Ping(ctx context.Context) error {
	_, err := c.doSimple(ctx, []string{"PING"})
	return err
}

func (c Client) EvalIntArray(ctx context.Context, script string, keys []string, args []string) ([]int64, error) {
	command := []string{"EVAL", script, strconv.Itoa(len(keys))}
	command = append(command, keys...)
	command = append(command, args...)

	value, err := c.do(ctx, command)
	if err != nil {
		return nil, err
	}

	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("unexpected eval response type %T", value)
	}

	values := make([]int64, 0, len(items))
	for _, item := range items {
		number, ok := item.(int64)
		if !ok {
			return nil, fmt.Errorf("unexpected eval item type %T", item)
		}
		values = append(values, number)
	}

	return values, nil
}

func (c Client) IncrBy(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	value, err := c.do(ctx, []string{"INCRBY", key, strconv.FormatInt(delta, 10)})
	if err != nil {
		return 0, err
	}

	total, ok := value.(int64)
	if !ok {
		return 0, fmt.Errorf("unexpected INCRBY response type %T", value)
	}

	if ttl > 0 {
		if _, err := c.doSimple(ctx, []string{"EXPIRE", key, strconv.Itoa(int(ttl.Seconds()))}); err != nil {
			return 0, err
		}
	}

	return total, nil
}

func (c Client) doSimple(ctx context.Context, command []string) (string, error) {
	value, err := c.do(ctx, command)
	if err != nil {
		return "", err
	}

	switch v := value.(type) {
	case string:
		return v, nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	default:
		return "", fmt.Errorf("unexpected redis response type %T", value)
	}
}

func (c Client) do(ctx context.Context, command []string) (any, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = conn.Close()
	}()

	if err := writeCommand(conn, command); err != nil {
		return nil, err
	}

	reader := bufio.NewReader(conn)
	return readRESP(reader)
}

func (c Client) dial(ctx context.Context) (net.Conn, error) {
	dialer := net.Dialer{Timeout: c.dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", c.address)
	if err != nil {
		return nil, err
	}

	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			_ = conn.Close()
			return nil, err
		}
	} else {
		if err := conn.SetDeadline(time.Now().Add(c.dialTimeout)); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}

	if c.password != "" {
		if _, err := c.doHandshake(conn, []string{"AUTH", c.password}); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}

	if c.db > 0 {
		if _, err := c.doHandshake(conn, []string{"SELECT", strconv.Itoa(c.db)}); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}

	return conn, nil
}

func (c Client) doHandshake(conn net.Conn, command []string) (any, error) {
	if err := writeCommand(conn, command); err != nil {
		return nil, err
	}

	reader := bufio.NewReader(conn)
	return readRESP(reader)
}

func writeCommand(conn net.Conn, command []string) error {
	var builder strings.Builder
	builder.WriteString("*")
	builder.WriteString(strconv.Itoa(len(command)))
	builder.WriteString("\r\n")
	for _, part := range command {
		builder.WriteString("$")
		builder.WriteString(strconv.Itoa(len(part)))
		builder.WriteString("\r\n")
		builder.WriteString(part)
		builder.WriteString("\r\n")
	}

	_, err := conn.Write([]byte(builder.String()))
	return err
}

func readRESP(reader *bufio.Reader) (any, error) {
	return respReader{reader: reader}.read()
}

func (r respReader) read() (any, error) {
	prefix, line, err := r.readHeader()
	if err != nil {
		return nil, err
	}

	switch prefix {
	case '+':
		return line, nil
	case '-':
		return nil, errors.New(line)
	case ':':
		return strconv.ParseInt(line, 10, 64)
	case '$':
		return r.readBulkString(line)
	case '*':
		return r.readArray(line)
	default:
		return nil, fmt.Errorf("unsupported redis response prefix %q", prefix)
	}
}

func (r respReader) readHeader() (byte, string, error) {
	prefix, err := r.reader.ReadByte()
	if err != nil {
		return 0, "", err
	}

	line, err := r.reader.ReadString('\n')
	if err != nil {
		return 0, "", err
	}
	return prefix, strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}

func (r respReader) readBulkString(line string) (any, error) {
	size, err := strconv.Atoi(line)
	if err != nil {
		return nil, err
	}
	if size < 0 {
		return "", nil
	}

	buffer := make([]byte, size+2)
	if _, err := io.ReadFull(r.reader, buffer); err != nil {
		return nil, err
	}
	return string(buffer[:size]), nil
}

func (r respReader) readArray(line string) (any, error) {
	size, err := strconv.Atoi(line)
	if err != nil {
		return nil, err
	}
	if size < 0 {
		return []any{}, nil
	}

	items := make([]any, 0, size)
	for i := 0; i < size; i++ {
		item, err := r.read()
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
