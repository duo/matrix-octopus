# matrix-octopus
A Matrix-Octopus puppeting bridge based on [mautrix-go](https://github.com/mautrix/go).

### Documentation

Some quick links:

* [Bridge setup](https://docs.mau.fi/bridges/go/setup.html)
* [Docker](https://hub.docker.com/r/lxduo/matrix-octopus)

### Features & roadmap

* Matrix → Octopus
  * [ ] Message types
    * [x] Text
	  * [x] Image
	  * [x] Sticker
	  * [x] Video
	  * [x] Audio
    * [x] File
    * [x] Mention
    * [x] Reply
    * [x] Location
  * [x] Chat types
	  * [x] Direct
	  * [x] Room
  * [ ] Presence
  * [ ] Redaction
  * [ ] Group actions
    * [ ] Join
    * [ ] Invite
    * [ ] Leave
    * [ ] Kick
	  * [ ] Mute
  * [ ] Room metadata
    * [ ] Name
    * [ ] Avatar
    * [ ] Topic
  * [ ] User metadata
    * [ ] Name
    * [ ] Avatar

* Octopus → Matrix
  * [ ] Message types
    * [x] Text
    * [x] Image
    * [x] Sticker
    * [x] Video
    * [x] Audio
    * [x] File
    * [x] Mention
    * [x] Reply
    * [x] Location
  * [ ] Chat types
    * [x] Private
    * [x] Group
  * [ ] Presence
  * [x] Redaction
  * [ ] Group actions
    * [ ] Invite
    * [ ] Join
    * [ ] Leave
    * [ ] Kick
	* [ ] Mute
  * [ ] Group metadata
    * [x] Name
    * [x] Avatar
	* [ ] Topic
  * [x] User metadata
    * [x] Name
    * [x] Avatar
  * [ ] Login types
	  * [ ] Password
	  * [x] QR code

* Misc
  * [ ] Automatic portal creation
    * [ ] After login
    * [ ] When added to group
    * [x] When receiving message
  * [x] Double puppeting
