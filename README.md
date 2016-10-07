Tolxanka
========

The Tolxanka imageboard. This is not currently in production anywhere. Feel
free to use it, but expect a fair amount of bugs.

Features
========

- Threads organized by user definable tags, rather than boards
- Websocket-based realtime thread auto-update
- Conversation folding
- Conversation reply coloring
- `<video>` element support (webm)
- `<audio>` element support (vorbis, mp3)
- Client-side comment length / file type / file size checking
- Reply anchor links
- Upload progress indicator
- Compact thread layout
- Han characters used as per-thread capcodes
- (Futaba-style) thread summary view
- Catalog view
- Basic spam filtering
- Post reporting and report queues
- User banning
- Several levels of caching of templated HTML for higher responsiveness
- Persistence using SQLite
- Firefox admin extension
- PGP challenge/response admin authentication
- Standard admin tasks (thread or post deletion, locking, stickying)
- Fine-grained, administratively defined staff roles
- Configurable user action restriction thresholds
- Regex-based post filtering / auto-banning
- MD5 image/video/audio blacklisting

Anti-Features
=============

Relatively important (albeit not critical) things that have yet to be added.

- Bans are currently irrevocable before their expire date,
  aside from manually deleting them from sqlite
- Bans cannot be appealed by users
- Client-side JavaScript functionality is not yet user configurable

Basic Setup
===========

Dependencies
------------

- ffmpeg
- ffprobe
- sqlite
- GPG or some other kind of PGP software for administration
- Some recent version of go is required to build from source

Installation
------------

2. `go get github.com/nkeronkow/tolxanka/...`
3. `cd $GOPATH/src/github.com/nkeronkow/tolxanka/`
4. `go build -o tolxanka`
5. Update the ValidReferers field in config/settings.toml to match your
   server address.

Setting Up Administrative Roles
-------------------------------

- First, create a gpg keypair using `gpg --gen-key`.
- Enter your admin info and public key into config/admin.toml, following the
  same format as the example entry.
- Install tlxadmin/txladmin.xpi.
- You will be presented with a "Login" link in the bottom right of any query
  or thread page. Click it to log in. You'll be given random challenge text.
- Run `gpg -asb` in a terminal window.
- Copy paste the challenge text into the terminal window and Ctrl-D twice.
- Copy paste the generated response in to the bottommost field.
- Enter your staff name in the middle field.
- Submit to login and gain admin rights.
- Refer back to config/admin.toml to revoke/add rights or add new staff users.

Questions / Bugs / Issues
=========================

Please report any of the above in the issues tracker.



