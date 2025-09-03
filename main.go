package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
)

const REDIRECT_URL string = "http://127.0.0.1:8080/callback"

var (
	auth = spotifyauth.New(
		spotifyauth.WithRedirectURL(REDIRECT_URL),
		spotifyauth.WithScopes(spotifyauth.ScopeUserReadPrivate),
		spotifyauth.WithScopes(spotifyauth.ScopeUserLibraryRead),
		// spotifyauth.WithScopes(spotifyauth.ScopePlaylistModifyPublic),
		// spotifyauth.WithScopes(spotifyauth.ScopePlaylistModifyPrivate),
	)
	ch    = make(chan *spotify.Client)
	state = "abc123"
)

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
	user, err := client.CurrentUser(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("You are logged in as:", user.ID)

	var trackPage *spotify.SavedTrackPage
	var offset int
	var returned int = -1
	// var tracks []spotify.SavedTrack
	for returned == -1 || returned > 0 {
		trackPage, err = client.CurrentUsersTracks(context.Background(), spotify.Limit(50), spotify.Offset(offset))
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
	ch <- client
}
