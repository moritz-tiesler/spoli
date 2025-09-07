package chrome

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/chromedp/chromedp"
	"github.com/zmb3/spotify/v2"
	"golang.org/x/oauth2"
)

type Instance struct {
	// taskCtx     context.Context
	// taskCancel  context.CancelFunc
	allocCtx    context.Context
	allocCancel context.CancelFunc
}

func New(
	spotifyClient *spotify.Client,
	tok *oauth2.Token,
	remoteChromeUrl string,
) *Instance {

	allocCtx, allocCancel := chromedp.NewRemoteAllocator(
		context.Background(),
		remoteChromeUrl,
	)

	return &Instance{
		allocCtx,
		allocCancel,
	}
}

func (i *Instance) Start(host string) error {
	ts := chromedp.Tasks{
		chromedp.Navigate(host),
		chromedp.WaitReady(`#deviceId`),
	}
	err := i.Run(ts)
	if err != nil {
		return fmt.Errorf("error navigating to %s: %s", host, err)
	}

	var buf []byte
	err = i.Snap(".", &buf)
	if err != nil {
		return fmt.Errorf("error capturing screen: %s", err)
	}

	if err := os.WriteFile("func_start.png", buf, 0o644); err != nil {
		return fmt.Errorf("error saving screenshot: %s", err)
	}

	return nil

}

func (i *Instance) Click(elementId string) error {
	ts := chromedp.Tasks{
		chromedp.WaitVisible(fmt.Sprintf(`#%s`, elementId)),
		chromedp.Click(fmt.Sprintf(`#%s`, elementId)),
	}

	return i.Run(ts)
}

func (i *Instance) Run(tasks chromedp.Tasks) error {
	taskCtx, cancelTask := chromedp.NewContext(i.allocCtx, chromedp.WithDebugf(log.Printf))
	defer cancelTask()
	return chromedp.Run(taskCtx, tasks)
}

func (i *Instance) Snap(path string, buf *[]byte) error {
	return i.Run(chromedp.Tasks{
		chromedp.CaptureScreenshot(buf),
	})
}
