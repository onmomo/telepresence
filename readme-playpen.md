# APro Dev CLI and Daemon (Playpen)

Since I don't feel like thinking about naming, I've arbitrarily decide to use the name Playpen. We should come up with something better.

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

## Remaining

Need to add UX for

- Installation
- Startup (?)
