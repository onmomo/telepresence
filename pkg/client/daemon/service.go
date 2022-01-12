package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	empty "google.golang.org/protobuf/types/known/emptypb"

	"github.com/datawire/dlib/derror"
	"github.com/datawire/dlib/dgroup"
	"github.com/datawire/dlib/dhttp"
	"github.com/datawire/dlib/dlog"
	"github.com/telepresenceio/telepresence/rpc/v2/common"
	rpc "github.com/telepresenceio/telepresence/rpc/v2/daemon"
	"github.com/telepresenceio/telepresence/rpc/v2/manager"
	"github.com/telepresenceio/telepresence/v2/pkg/client"
	"github.com/telepresenceio/telepresence/v2/pkg/client/logging"
	"github.com/telepresenceio/telepresence/v2/pkg/client/scout"
	"github.com/telepresenceio/telepresence/v2/pkg/filelocation"
	"github.com/telepresenceio/telepresence/v2/pkg/log"
	"github.com/telepresenceio/telepresence/v2/pkg/proc"
)

const ProcessName = "daemon"
const titleName = "Daemon"

var help = `The Telepresence ` + titleName + ` is a long-lived background component that manages
connections and network state.

Launch the Telepresence ` + titleName + `:
    sudo telepresence service

Examine the ` + titleName + `'s log output in
    ` + filepath.Join(func() string { dir, _ := filelocation.AppUserLogDir(context.Background()); return dir }(), ProcessName+".log") + `
to troubleshoot problems.
`

// service represents the state of the Telepresence Daemon
type service struct {
	rpc.UnsafeDaemonServer
	quit          context.CancelFunc
	connectCh     chan *rpc.OutboundInfo
	connectErrCh  chan error
	sessionLock   sync.Mutex
	session       *session
	timedLogLevel log.TimedLevel

	scout *scout.Reporter
}

// Command returns the telepresence sub-command "daemon-foreground"
func Command() *cobra.Command {
	return &cobra.Command{
		Use:    ProcessName + "-foreground <logging dir> <config dir>",
		Short:  "Launch Telepresence " + titleName + " in the foreground (debug)",
		Args:   cobra.ExactArgs(2),
		Hidden: true,
		Long:   help,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.Context(), args[0], args[1])
		},
	}
}

func (d *service) Version(_ context.Context, _ *empty.Empty) (*common.VersionInfo, error) {
	return &common.VersionInfo{
		ApiVersion: client.APIVersion,
		Version:    client.Version(),
	}, nil
}

func (d *service) Status(_ context.Context, _ *empty.Empty) (*rpc.DaemonStatus, error) {
	d.sessionLock.Lock()
	defer d.sessionLock.Unlock()
	r := &rpc.DaemonStatus{}
	if d.session != nil {
		r.OutboundConfig = d.session.getInfo()
	}
	return r, nil
}

func (d *service) Quit(ctx context.Context, _ *empty.Empty) (*empty.Empty, error) {
	dlog.Debug(ctx, "Received gRPC Quit")
	d.quit()
	return &empty.Empty{}, nil
}

func (d *service) SetDnsSearchPath(ctx context.Context, paths *rpc.Paths) (*empty.Empty, error) {
	session, err := d.currentSession()
	if err != nil {
		return nil, err
	}
	session.SetSearchPath(ctx, paths.Paths, paths.Namespaces)
	return &empty.Empty{}, nil
}

func (d *service) Connect(ctx context.Context, info *rpc.OutboundInfo) (*empty.Empty, error) {
	dlog.Debug(ctx, "Received gRPC Connect")
	d.sessionLock.Lock()
	defer d.sessionLock.Unlock()
	if d.session != nil {
		return nil, status.Error(codes.AlreadyExists, "an active session exists")
	}
	select {
	case <-ctx.Done():
		return &empty.Empty{}, nil
	case d.connectCh <- info:
	}
	select {
	case <-ctx.Done():
	case err := <-d.connectErrCh:
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	return &empty.Empty{}, nil
}

func (d *service) Disconnect(ctx context.Context, _ *empty.Empty) (*empty.Empty, error) {
	dlog.Debug(ctx, "Received gRPC Disconnect")
	d.sessionLock.Lock()
	defer d.sessionLock.Unlock()
	if d.session != nil {
		d.session.cancel()
	}
	return &empty.Empty{}, nil
}

func (d *service) currentSession() (*session, error) {
	d.sessionLock.Lock()
	defer d.sessionLock.Unlock()
	if d.session == nil {
		return nil, status.Error(codes.Unavailable, "no active session")
	}
	return d.session, nil
}

func (d *service) GetClusterSubnets(ctx context.Context, _ *empty.Empty) (*rpc.ClusterSubnets, error) {
	session, err := d.currentSession()
	if err != nil {
		return nil, err
	}

	// The manager can sometimes send the different subnets in different Sends, but after 5 seconds of listening to it
	// we should expect to have everything
	tCtx, tCancel := context.WithTimeout(ctx, 5*time.Second)
	defer tCancel()
	infoStream, err := session.managerClient.WatchClusterInfo(tCtx, session.session)
	if err != nil {
		return nil, err
	}
	podSubnets := []*manager.IPNet{}
	svcSubnets := []*manager.IPNet{}
	for {
		mgrInfo, err := infoStream.Recv()
		if err != nil {
			if tCtx.Err() == nil && !errors.Is(err, io.EOF) {
				return nil, err
			}
			break
		}
		if mgrInfo.ServiceSubnet != nil {
			svcSubnets = append(svcSubnets, mgrInfo.ServiceSubnet)
		}
		podSubnets = append(podSubnets, mgrInfo.PodSubnets...)
	}
	return &rpc.ClusterSubnets{PodSubnets: podSubnets, SvcSubnets: svcSubnets}, nil
}

func (d *service) SetLogLevel(ctx context.Context, request *manager.LogLevelRequest) (*empty.Empty, error) {
	duration := time.Duration(0)
	if request.Duration != nil {
		duration = request.Duration.AsDuration()
	}
	return &empty.Empty{}, logging.SetAndStoreTimedLevel(ctx, d.timedLogLevel, request.LogLevel, duration, ProcessName)
}

func reloadConfig(c context.Context) error {
	newCfg, err := client.LoadConfig(c)
	if err != nil {
		return err
	}
	client.ReplaceConfig(c, newCfg)
	log.SetLevel(c, newCfg.LogLevels.RootDaemon.String())
	dlog.Info(c, "Configuration reloaded")
	return nil
}

func (d *service) configReload(c context.Context) error {
	configFile := client.GetConfigFile(c)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	// The directory containing the config file must be watched because editing
	// the file will typically end with renaming the original and then creating
	// a new file. A watcher that follows the inode will not see when the new
	// file is created.
	if err = watcher.Add(filepath.Dir(configFile)); err != nil {
		return err
	}

	// The delay timer will initially sleep forever. It's reset to a very short
	// delay when the file is modified.
	delay := time.AfterFunc(time.Duration(math.MaxInt64), func() {
		if err := reloadConfig(c); err != nil {
			dlog.Error(c, err)
		}
	})
	defer delay.Stop()

	for {
		select {
		case <-c.Done():
			return nil
		case err = <-watcher.Errors:
			dlog.Error(c, err)
		case event := <-watcher.Events:
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 && event.Name == configFile {
				// The config file was created or modified. Let's defer the load just a little bit
				// in case there are more modifications (a write out with vi will typically cause
				// one CREATE event and at least one WRITE event).
				delay.Reset(5 * time.Millisecond)
			}
		}
	}
}

// manageSessions is the counterpart to the Connect method. It reads the connectCh, creates
// a session and writes a reply to the connectErrCh. The session is then started if it was
// successfully created.
func (d *service) manageSessions(c context.Context) error {
	// The d.quit is called when we receive a Quit. Since it
	// terminates this function, it terminates the whole process.
	c, d.quit = context.WithCancel(c)
	for {
		// Wait for a connection request
		var oi *rpc.OutboundInfo
		select {
		case <-c.Done():
			return nil
		case oi = <-d.connectCh:
		}

		// Respond by setting the session and returning the error (or nil
		// if everything is ok)
		var err error
		d.session, err = newSession(c, d.scout, oi)
		select {
		case <-c.Done():
			return nil
		case d.connectErrCh <- err:
		}
		if err != nil {
			continue
		}

		// Run the session synchronously and ensure that it is cleaned
		// up properly when the context is cancelled
		func(c context.Context) {
			defer func() {
				d.sessionLock.Lock()
				d.session = nil
				d.sessionLock.Unlock()
			}()

			// The d.session.cancel is called from Disconnect
			c, d.session.cancel = context.WithCancel(c)
			if err := d.session.run(c); err != nil {
				dlog.Error(c, err)
			}
		}(c)
	}
}

func (d *service) serveGrpc(c context.Context, l net.Listener) error {
	defer func() {
		// Error recovery.
		if perr := derror.PanicToError(recover()); perr != nil {
			dlog.Error(c, perr)
		}
	}()

	var opts []grpc.ServerOption
	cfg := client.GetConfig(c)
	if !cfg.Grpc.MaxReceiveSize.IsZero() {
		if mz, ok := cfg.Grpc.MaxReceiveSize.AsInt64(); ok {
			opts = append(opts, grpc.MaxRecvMsgSize(int(mz)))
		}
	}
	svc := grpc.NewServer(opts...)
	rpc.RegisterDaemonServer(svc, d)

	sc := &dhttp.ServerConfig{
		Handler: svc,
	}
	dlog.Info(c, "gRPC server started")
	err := sc.Serve(c, l)
	if err != nil {
		dlog.Errorf(c, "gRPC server ended with: %v", err)
	} else {
		dlog.Debug(c, "gRPC server ended")
	}
	return err
}

// run is the main function when executing as the daemon
func run(c context.Context, loggingDir, configDir string) error {
	if !proc.IsAdmin() {
		return fmt.Errorf("telepresence %s must run with elevated privileges", ProcessName)
	}

	// seed random generator (used when shuffling IPs)
	rand.Seed(time.Now().UnixNano())

	// Spoof the AppUserLogDir and AppUserConfigDir so that they return the original user's
	// directories rather than directories for the root user.
	c = filelocation.WithAppUserLogDir(c, loggingDir)
	c = filelocation.WithAppUserConfigDir(c, configDir)

	cfg, err := client.LoadConfig(c)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	c = client.WithConfig(c, cfg)

	c = dgroup.WithGoroutineName(c, "/"+ProcessName)
	c, err = logging.InitContext(c, ProcessName, logging.RotateDaily)
	if err != nil {
		return err
	}

	dlog.Info(c, "---")
	dlog.Infof(c, "Telepresence %s %s starting...", ProcessName, client.DisplayVersion())
	dlog.Infof(c, "PID is %d", os.Getpid())
	dlog.Info(c, "")

	// Listen on domain unix domain socket or windows named pipe. The listener must be opened
	// before other tasks because the CLI client will only wait for a short period of time for
	// the socket/pipe to appear before it gives up.
	grpcListener, err := client.ListenSocket(c, ProcessName, client.DaemonSocketName)
	if err != nil {
		return err
	}
	defer func() {
		_ = client.RemoveSocket(grpcListener)
	}()
	dlog.Debug(c, "Listener opened")

	d := &service{
		scout:         scout.NewReporter(c, "daemon"),
		timedLogLevel: log.NewTimedLevel(cfg.LogLevels.RootDaemon.String(), log.SetLevel),
		connectCh:     make(chan *rpc.OutboundInfo),
		connectErrCh:  make(chan error),
	}
	if err = logging.LoadTimedLevelFromCache(c, d.timedLogLevel, ProcessName); err != nil {
		return err
	}

	g := dgroup.NewGroup(c, dgroup.GroupConfig{
		SoftShutdownTimeout:  2 * time.Second,
		EnableSignalHandling: true,
		ShutdownOnNonError:   true,
	})

	// Add a reload function that triggers on create and write of the config.yml file.
	g.Go("config-reload", d.configReload)
	g.Go("session", d.manageSessions)
	g.Go("server-grpc", func(c context.Context) error { return d.serveGrpc(c, grpcListener) })
	g.Go("metriton", d.scout.Run)
	err = g.Wait()
	if err != nil {
		dlog.Error(c, err)
	}
	return err
}
