module ChatRoomGameDemo/weiqiGame

go 1.21

require (
	ChatRoomGameDemo/nichijouSDK v0.0.0
	github.com/gorilla/websocket v1.5.1
)

require golang.org/x/net v0.17.0 // indirect

replace ChatRoomGameDemo/nichijouSDK => ../nichijouSDK
