package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2"
)

const REDIRECT_URL string = "http://127.0.0.1:8080/callback"

// TODO: use PKCE
// users will not have to store their client secret
var (
	auth = spotifyauth.New(
		spotifyauth.WithRedirectURL(REDIRECT_URL),
		spotifyauth.WithScopes(spotifyauth.ScopeUserReadPrivate),
		spotifyauth.WithScopes(spotifyauth.ScopeUserLibraryRead),
		// spotifyauth.WithScopes(spotifyauth.ScopePlaylistModifyPublic),
		// spotifyauth.WithScopes(spotifyauth.ScopePlaylistModifyPrivate),
	)
	ch = make(chan struct {
		c *spotify.Client
		t *oauth2.Token
	},
	)
	state = "abc123"
)

// TODO: on app startup
// load stored access token, compare expiry date
// if expired send old token to spotify to get new token
// store new token

func main() {
	http.HandleFunc("/callback", completeAuth)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Println("Got request for:", r.URL.String())
	})

	go func() {
		err := http.ListenAndServe(":8080", nil)
		if err != nil {
			log.Fatal(err)
		}
	}()

	url := auth.AuthURL(state)
	fmt.Println("Please log in to Spotify by visiting the following page in your browser:", url)

	// wait for auth to complete
	client := <-ch

	// use the client to make calls that require authorization
	user, err := client.c.CurrentUser(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("You are logged in as:", user.ID)

	var trackPage *spotify.SavedTrackPage
	var offset int
	var returned int = -1
	// can save one request by checking returned < limit
	for returned == -1 || returned > 0 {
		trackPage, err = client.c.CurrentUsersTracks(context.Background(), spotify.Limit(50), spotify.Offset(offset))
		tracks := trackPage.Tracks
		if err != nil {
			log.Fatalf("could not get saved tracks: %v", err)
		}
		for _, t := range tracks {
			fmt.Println(t.Name)
		}
		returned = len(tracks)
		offset += returned
	}
	fmt.Println(offset)

	done := make(chan struct{})
	runChrome(client.c, client.t, done)
	<-done

}

func completeAuth(w http.ResponseWriter, r *http.Request) {
	tok, err := auth.Token(r.Context(), state, r)
	if err != nil {
		http.Error(w, "Couldn't get token", http.StatusForbidden)
		log.Fatal(err)
	}
	if st := r.FormValue("state"); st != state {
		http.NotFound(w, r)
		log.Fatalf("State mismatch: %s != %s\n", st, state)
	}

	// use the token to get an authenticated client
	client := spotify.New(auth.Client(r.Context(), tok))
	fmt.Fprintf(w, "Login Completed!")
	ch <- struct {
		c *spotify.Client
		t *oauth2.Token
	}{
		client,
		tok,
	}
}

const spotifySDKURL = "https://sdk.scdn.co/spotify-player.js"

func runChrome(spotifyClient *spotify.Client, tok *oauth2.Token, done chan struct{}) {

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(
		context.Background(),
		chromedp.Headless,
		chromedp.NoSandbox,
	)
	defer cancelAlloc()

	taskCtx, cancelTask := chromedp.NewContext(allocCtx)
	defer cancelTask()

	// Channel to receive the device ID from the browser's JavaScript
	deviceIDChan := make(chan string, 1)

	// Mutex to protect writing to the deviceIDChan from concurrent goroutines
	var mu sync.Mutex

	// Listen for events from the browser target
	chromedp.ListenTarget(taskCtx, func(ev interface{}) {
		switch ev := ev.(type) {
		case *page.EventJavascriptDialogOpening:
			log.Printf("BROWSER DIALOG: %s - %s", ev.Type, ev.Message)
			go func() {
				if err := chromedp.Run(taskCtx,
					page.HandleJavaScriptDialog(true), // Accept the dialog
				); err != nil {
					log.Printf("Error handling dialog: %v", err)
				}
			}()
		case *runtime.EventBindingCalled:
			// This is where we handle calls from JavaScript to our Go-exposed function
			if ev.Name == "sendDeviceID" { // Check the binding name
				var args []string // Assuming the JS function sends a single string argument
				if err := json.Unmarshal([]byte(ev.Payload), &args); err != nil {
					log.Printf("Error unmarshaling binding payload: %v", err)
					return
				}
				if len(args) > 0 {
					log.Printf("Received device ID from browser via binding: %s", args[0])
					mu.Lock()
					select {
					case deviceIDChan <- args[0]: // Send to our Go channel
						// Successfully sent
					default:
						log.Println("Device ID channel is already full, ignoring subsequent calls.")
					}
					mu.Unlock()
				}
			}
		case *runtime.EventConsoleAPICalled:
			if ev.Type == runtime.APITypeError {
				log.Fatal(ev)
			}

		}
	})

	// JavaScript to inject and run in the browser
	// TODO: this is trash, follow this
	// https://developer.spotify.com/documentation/web-playback-sdk/tutorials/getting-started
	spotifySDKInitJS := fmt.Sprintf(`
		// No console.log here to avoid generating unnecessary console events
		 window.onSpotifyWebPlaybackSDKReady = () => {
		 	const player = new Spotify.Player({
		 		name: 'Chromedp Headless Player',
		 		getOAuthToken: cb => { cb('%s'); }, // Inject the access token here
		 		volume: 0.5
		 	});

		 	player.addListener('ready', ({ device_id }) => {
		 		window.sendDeviceID(device_id); // Call the exposed Go binding
		 	});

		 	// Still keep other listeners for error handling, but can remove their console.log if desired
		 	player.addListener('not_ready', ({ device_id }) => {});
		 	player.addListener('player_state_changed', (state) => {});
		 	player.addListener('initialization_error', ({ message }) => { console.error('Initialization Error:', message); }); // Keeping error logs
		 	player.addListener('authentication_error', ({ message }) => { console.error('Authentication Error:', message); }); // Keeping error logs
		 	player.addListener('account_error', ({ message }) => { console.error('Account Error:', message); }); // Keeping error logs
		 	player.addListener('playback_error', ({ message }) => { console.error('Playback Error:', message); }); // Keeping error logs

		 	player.connect().then(success => {
		 		if (!success) {
		 			console.error('Failed to connect to Spotify Web Playback SDK.');
		 		}
		 	}).catch(err => {
		 		console.error('Error connecting to SDK:', err);
		 	});
		 };
	`, tok.AccessToken)

	// spotifySDKInitJS := fmt.Sprintf(`window.onSpotifyWebPlaybackSDKReady = () => {
	// 	const token = '%s';
	// 	const player = new Spotify.Player({
	// 		name: 'Web Playback SDK Quick Start Player',
	// 		getOAuthToken: cb => { cb(token); },
	// 		volume: 0.5
	// 	});
	// };`, tok.AccessToken)

	var receivedDeviceID string
	err := chromedp.Run(taskCtx,
		chromedp.Navigate("about:blank"),

		// Register the binding named "sendDeviceID".
		chromedp.ActionFunc(func(ctx context.Context) error {
			fmt.Println("running register")
			return runtime.AddBinding("sendDeviceID").Do(ctx)
		}),

		// Inject the Spotify Web Playback SDK script first
		chromedp.ActionFunc(func(ctx context.Context) error {
			fmt.Println("running inject playback sdk")
			_, err := page.AddScriptToEvaluateOnNewDocument(spotifySDKURL).Do(ctx)
			if err != nil {
				return fmt.Errorf("failed to add SDK script: %w", err)
			}
			return nil
		}),

		// Then inject our custom logic that initializes the player
		chromedp.Evaluate(spotifySDKInitJS, nil),

		// Wait for the device ID to be received from the browser via the channel
		chromedp.ActionFunc(func(ctx context.Context) error {
			fmt.Println("running wait for device id")
			select {
			case id := <-deviceIDChan:
				receivedDeviceID = id
				log.Printf("Successfully captured device ID: %s", receivedDeviceID)
				return nil
			case <-time.After(10 * time.Second):
				return fmt.Errorf("timed out waiting for Spotify player device ID")
			}
		}),
	)

	if err != nil {
		log.Println(spotifySDKInitJS)
		log.Fatalf("Chromedp error: %v", err)
	}

	if receivedDeviceID == "" {
		log.Fatal("Did not receive a device ID. Cannot proceed with playback.")
	}

	spId := spotify.ID(receivedDeviceID)

	log.Printf("Chromedp setup complete. Device ID: %s", receivedDeviceID)

	// Now use the Spotify Web API to control playback
	err = spotifyClient.TransferPlayback(taskCtx, spId, true)
	if err != nil {
		log.Fatalf("Failed to transfer playback: %v", err)
	}
	log.Printf("Playback transferred to headless device: %s", receivedDeviceID)

	trackURI := spotify.URI("spotify:track:2PpE1b4d320mSgGkM2QdGv") // Example: "Blinding Lights"
	err = spotifyClient.PlayOpt(taskCtx, &spotify.PlayOptions{
		DeviceID: &spId,
		URIs:     []spotify.URI{trackURI},
	})
	if err != nil {
		log.Fatalf("Failed to play track: %v", err)
	}
	log.Printf("Playing track: %s on device: %s", trackURI, receivedDeviceID)

	time.Sleep(10 * time.Second)

	err = spotifyClient.Pause(taskCtx)
	if err != nil {
		log.Printf("Failed to pause playback: %v", err)
	}
	log.Println("Playback paused.")

	time.Sleep(2 * time.Second)

	err = spotifyClient.Play(taskCtx)
	if err != nil {
		log.Printf("Failed to resume playback: %v", err)
	}
	log.Println("Playback resumed.")

	time.Sleep(5 * time.Second)

	log.Println("Finished. Closing browser...")

	done <- struct{}{}
}
