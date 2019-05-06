# APro Dev CLI and Daemon (Playpen)

Since I don't feel like thinking about naming, I've arbitrarily decide to use the name Playpen. We should come up with something better. We have a proof-of-concept implementation to play with. It implements some of the target UX (see below), but not the intercept stuff yet.


## Proof of Concept

You can get a copy of the distribution ZIP file from Abhay.

### Install

Unzip the distribution somewhere in your shell path. I use `~/datawire/bin` in my example but `/usr/local/bin` should be fine too.

```console
$ unzip -l ~/Downloads/playpen-15-g87bce02.zip
Archive:  /Users/ark3/Downloads/playpen-15-g87bce02.zip
  Length      Date    Time    Name
---------  ---------- -----   ----
     1559  05-03-2019 17:54   playpen
    63978  05-03-2019 17:54   playpend
      319  05-03-2019 17:54   pp-launch
 43978020  05-03-2019 17:54   pp-teleproxy-darwin-amd64
 38171058  05-03-2019 17:54   pp-teleproxy-linux-amd64
---------                     -------
 82214934                     5 files

$ (cd ~/datawire/bin && unzip ~/Downloads/playpen-15-g87bce02.zip)
Archive:  /Users/ark3/Downloads/playpen-15-g87bce02.zip
  inflating: playpen
  inflating: playpend
  inflating: pp-launch
  inflating: pp-teleproxy-darwin-amd64  
  inflating: pp-teleproxy-linux-amd64  

$ type playpen
playpen is /Users/ark3/datawire/bin/playpen
```

### Launch

You can start Playpen Daemon using `sudo playpend` directly if you'd like, perhaps because you want to see the minimal output it generates for some reason. But using the `pp-launch` script is easier and gives you back your terminal. The daemon's useful output is available in `/tmp/playpen.log`.

```console
$ playpen
Connect to /var/run/playpen.socket failed: [Errno 2] No such file or directory

The server doesn't appear to be running.
Take a look at /tmp/playpen.log for more information.
You can start the server using pp-launch.

playpen: Could not connect to server

$ pp-launch
Launching Playpen Daemon 15-g87bce02 using sudo...
[sudo ark3@timonium] Password:
Launched! See /tmp/playpen.log for diagnostics.

$ tail -3 /tmp/playpen.log
   5.1 TEL | Network False --> True
   5.1 TEL | [5] Running: osascript -e 'display notification "(Playpen Daemon)" with title "Network False --> True"'
   5.2 TEL | [5] ran in 0.13 secs.

$ playpen
Not connected

$ playpen version
Playpen Client 15-g87bce02
Playpen Daemon 15-g87bce02

$ playpen connect
Connected to context tel-testing (https://35.184.12.241)

$ playpen
Connected
  Context:       tel-testing (https://35.184.12.241)
  Proxy:         ON (networking to the cluster is enabled)
  Interceptable: 0 deployments
  Intercepts:    ? total, 0 local

$ playpen disconnect
Disconnected
```

See the [Target UX section](#target-ux) below for further examples of how to use `playpen`. Note that intercept does not work yet.

### Quit

The only reason to quit the Playpen Daemon is to upgrade it, which you'll probably be doing often as I fix all the bugs you report.

Okay, that was a lie. Because Playpen Daemon uses Telepresence machinery for logging, the logfile (`/tmp/playpen.log`) will grow without bound. And because Teleproxy logs very verbosely, the logfile will grow quickly. It's probably a good idea to kill and restart Playpen Daemon every day.

```console
$ playpen status
Not connected

$ playpen quit
Playpen Daemon quitting...

$ playpen status
Connect to /var/run/playpen.socket failed: [Errno 2] No such file or directory

The server doesn't appear to be running.
Take a look at /tmp/playpen.log for more information.
You can start the server using pp-launch.

playpen: Could not connect to server

$ tail -3 /tmp/playpen.log
 359.1 TEL | (Cleanup) remove socket
 359.1 TEL | (Cleanup) Remove temporary directory
 359.1 TEL | (Cleanup) Save caches
```

### Upgrade

If it's time to upgrade Playpen, first quit the daemon as above. Now you can overwrite the binaries in-place by unzipping over the old ones. You can use `unzip -o` to overwrite files without prompting. On occasion I may hand off only the updated `playpend` binary, in which case you can just copy that over the old one.

With the new binaries in-place, start the daemon again using `pp-launch` as above.


## Target UX

```console
$ playpen status
Not connected

$ playpen connect
Connecting to cluster using context tel-testing...
Error: Playpen is not installed in this cluster
Not connected

$ KUBECONFIG=/dev/null playpen connect
Failed to connect: error: current-context is not set
Not connected

$ KUBECONFIG=build-aux/ambassador-pro.knaut.claim playpen connect
Connecting to cluster using context admin@kubernetes...
Connected

$ curl -k https://kubernetes/api/
{
  "kind": "APIVersions",
[...]

$ playpen status
Connected
  Context:       admin@kubernetes (https://3.86.31.224:6443)
  Proxy:         ON (networking to the cluster is enabled)
  Interceptable: 2 deployments
  Intercepts:    0 total, 0 local

$ playpen intercept
Interceptable deployments
  - echo:        0 intercepts
  - intercepted: 0 intercepts
No local intercepts

$ curl echo/ark3/moo
Request served by echo-5cb8959478-kpmdj

HTTP/1.1 GET /ark3/moo

Host: echo
X-Request-Id: 083100ab-04c8-44d6-80a9-db01a082d2bd
[...]

$ playpen intercept add echo -n Abhay1 -h ":path" -m ".*/ark3/.*"
Added intercept called Abhay1
  Deployment: echo
  - Header: :path
    Match:  .*/ark3/.*

$ playpen intercept list
Interceptable deployments
  - echo:        1 intercepts, 1 local
  - intercepted: 0 intercepts
Local intercepts
  - Name:       Abhay1
    Deployment: echo
    Header:     :path
    Match:      .*/ark3/.*

$ curl echo/ark3/moo
Request served by container_on_laptop

HTTP/1.1 GET /ark3/moo

Host: echo
X-Request-Id: c0404ed9-e55b-4e76-af20-817369e3050a
[...]

$ playpen status
Connected
  Context:       admin@kubernetes (https://3.86.31.224:6443)
  Proxy:         ON (networking to the cluster is enabled)
  Interceptable: 2 deployments
  Intercepts:    1 total, 1 local

$ playpen intercept remove Abhay1
Removed intercept called Abhay1

$ playpen disconnect
Disconnected
```
