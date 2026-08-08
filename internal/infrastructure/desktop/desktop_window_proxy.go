package desktop

import (
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// DesktopWindowProxy 以「同一個執行檔的另一個子命令」承載完整視窗。
//
// 為什麼是子程序而不是同一個程序裡的另一條 goroutine：選單列與視窗兩邊的 GUI
// 函式庫**都要求佔用 macOS 的主執行緒與主 run loop**，同一個程序裡無法並存。
// 分成兩個程序之後各自有自己的主執行緒，順帶得到崩潰隔離——視窗掛了不會把
// 選單列一起帶走。
//
// 父子之間的溝通是子程序的 stdin，一行一個網址。這也就是「視窗已開就重用」的
// 實作：不必問視窗在不在，寫進去就是了。
type DesktopWindowProxy struct {
	executablePath string

	mutex   sync.Mutex
	command *exec.Cmd
	input   io.WriteCloser
	exited  chan struct{}
}

// NewDesktopWindowProxy 建立 proxy。
//
// 「把視窗帶到最前」刻意不在這裡：視窗子程序收到新網址時會自己跳到前面。由它
// 自己來不需要任何系統權限，由外面來則需要輔助使用權限、而且拿不到時只能安靜
// 地失敗。
func NewDesktopWindowProxy(executablePath string) *DesktopWindowProxy {
	return &DesktopWindowProxy{executablePath: executablePath}
}

// Open 讓 targetURL 出現在完整視窗裡。
func (proxy *DesktopWindowProxy) Open(targetURL string) error {
	proxy.mutex.Lock()
	defer proxy.mutex.Unlock()

	if proxy.isRunning() {
		if _, err := io.WriteString(proxy.input, targetURL+"\n"); err != nil {
			// 寫不進去代表那個子程序其實已經不行了。收乾淨再開一個新的，
			// 好過回一個「視窗開著」但其實什麼都沒發生的謊。
			proxy.terminate()

			return proxy.start(targetURL)
		}

		return nil
	}

	return proxy.start(targetURL)
}

// Close 收掉視窗。
func (proxy *DesktopWindowProxy) Close() {
	proxy.mutex.Lock()
	defer proxy.mutex.Unlock()

	proxy.terminate()
}

// start 啟動一個新的視窗子程序。
func (proxy *DesktopWindowProxy) start(targetURL string) error {
	command := exec.Command(proxy.executablePath, "window", "--url="+targetURL)

	input, err := command.StdinPipe()
	if err != nil {
		return fmt.Errorf("could not open the window process: %w", err)
	}

	if err := command.Start(); err != nil {
		return fmt.Errorf("could not start the window process: %w", err)
	}

	exited := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(exited)
	}()

	proxy.command = command
	proxy.input = input
	proxy.exited = exited

	return nil
}

// isRunning 回報視窗子程序是否還活著。
func (proxy *DesktopWindowProxy) isRunning() bool {
	if proxy.command == nil || proxy.exited == nil {
		return false
	}

	select {
	case <-proxy.exited:
		return false
	default:
		return true
	}
}

// terminate 收掉子程序並清空狀態。
func (proxy *DesktopWindowProxy) terminate() {
	if proxy.command == nil {
		return
	}

	if proxy.input != nil {
		_ = proxy.input.Close()
	}

	if proxy.command.Process != nil {
		_ = proxy.command.Process.Kill()
	}

	if proxy.exited != nil {
		<-proxy.exited
	}

	proxy.command = nil
	proxy.input = nil
	proxy.exited = nil
}
