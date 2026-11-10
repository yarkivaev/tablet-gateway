package gateway

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const ScrcpyVersion = "3.3.3"

type Device struct {
	ID     string `json:"id"`
	Serial string `json:"serial"`
	Label  string `json:"label"`
}
type androidCapture struct {
	done           chan struct{}
	cancel         context.CancelFunc
	video, control net.Conn
	port, serial   string
	cmd            *exec.Cmd
	once           sync.Once
	write          sync.Mutex
}

func adb(ctx context.Context, serial string, args ...string) (string, error) {
	args = append([]string{"-s", serial}, args...)
	out, err := exec.CommandContext(ctx, "adb", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("adb: %w: %s", err, out)
	}
	return strings.TrimSpace(string(out)), nil
}
func AndroidFactory(devices map[string]Device, server string) Factory {
	return func(id string, send func(Frame)) (Capture, error) {
		d, ok := devices[id]
		if !ok {
			return nil, fmt.Errorf("unknown tablet")
		}
		ctx, cancel := context.WithCancel(context.Background())
		c := &androidCapture{cancel: cancel, serial: d.Serial, done: make(chan struct{})}
		fail := func(err error) (Capture, error) { c.Close(); return nil, err }
		setup, done := context.WithTimeout(ctx, 15*time.Second)
		defer done()
		if strings.Contains(d.Serial, ":") {
			if out, err := exec.CommandContext(setup, "adb", "connect", d.Serial).CombinedOutput(); err != nil {
				return fail(fmt.Errorf("connect: %w: %s", err, out))
			}
		}
		if _, err := adb(setup, d.Serial, "push", server, "/data/local/tmp/tablet-gateway.jar"); err != nil {
			return fail(err)
		}
		var random [4]byte
		rand.Read(random[:])
		random[0] &= 0x7f
		scid := hex.EncodeToString(random[:])
		port, err := adb(setup, d.Serial, "forward", "tcp:0", "localabstract:scrcpy_"+scid)
		if err != nil {
			return fail(err)
		}
		c.port = port
		c.cmd = exec.CommandContext(ctx, "adb", "-s", d.Serial, "shell", "CLASSPATH=/data/local/tmp/tablet-gateway.jar", "app_process", "/", "com.genymobile.scrcpy.Server", ScrcpyVersion, "scid="+scid, "tunnel_forward=true", "audio=false", "control=true", "video_codec=h264", "max_size=1280", "max_fps=15", "video_bit_rate=2000000", "clipboard_autosync=false", "send_device_meta=false", "send_codec_meta=false", "video_codec_options=profile=1,level=512,i-frame-interval=1")
		c.cmd.Stderr = os.Stderr
		if err = c.cmd.Start(); err != nil {
			return fail(err)
		}
		go c.cmd.Wait()
		for i := 0; i < 80; i++ {
			c.video, err = net.DialTimeout("tcp", "127.0.0.1:"+port, 200*time.Millisecond)
			if err == nil {
				c.video.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
				var dummy [1]byte
				_, err = io.ReadFull(c.video, dummy[:])
				if err == nil {
					c.video.SetReadDeadline(time.Time{})
					break
				}
				c.video.Close()
			}
			select {
			case <-setup.Done():
				return fail(setup.Err())
			case <-time.After(100 * time.Millisecond):
			}
		}
		if err != nil {
			return fail(err)
		}
		c.control, err = net.DialTimeout("tcp", "127.0.0.1:"+port, time.Second)
		if err != nil {
			return fail(err)
		}
		go io.Copy(io.Discard, c.control)
		go func() {
			defer c.Close()
			for {
				f, err := ReadFrame(c.video)
				if err != nil {
					return
				}
				send(f)
			}
		}()
		return c, nil
	}
}
func ReadFrame(r io.Reader) (Frame, error) {
	var h [12]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return Frame{}, err
	}
	n := binary.BigEndian.Uint32(h[8:])
	if n == 0 || n > 8*1024*1024 {
		return Frame{}, fmt.Errorf("invalid frame length %d", n)
	}
	pts := binary.BigEndian.Uint64(h[:8])
	f := Frame{Data: make([]byte, n), PTS: pts & ((uint64(1) << 62) - 1), Config: pts>>63 != 0}
	_, err := io.ReadFull(r, f.Data)
	return f, err
}
func (c *androidCapture) Close() {
	c.once.Do(func() {
		defer close(c.done)
		if c.video != nil {
			c.video.Close()
		}
		if c.control != nil {
			c.control.Close()
		}
		c.cancel()
		if c.port != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			adb(ctx, c.serial, "forward", "--remove", "tcp:"+c.port)
		}
	})
}
func (c *androidCapture) Control(data []byte) error {
	c.write.Lock()
	defer c.write.Unlock()
	c.control.SetWriteDeadline(time.Now().Add(time.Second))
	_, err := c.control.Write(data)
	return err
}

func (c *androidCapture) Done() <-chan struct{} { return c.done }
