package main

var SDK_INIT_JS string = `
		// No console.log here to avoid generating unnecessary console events
	window.onSpotifyWebPlaybackSDKReady = () => {
		console.log("player init...")
		const token = '%s';
		const player = new Spotify.Player({
			name: 'Web Playback SDK Quick Start Player',
			getOAuthToken: cb => { cb(token); },
			volume: 0.5
 		});
		console.log("player init done")
	};
	`
