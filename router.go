package main

import (
	"fmt"
	"github.com/alphagov/router/handlers"
	"github.com/alphagov/router/logger"
	"github.com/alphagov/router/triemux"
	"labix.org/v2/mgo"
	"net/http"
	"net/url"
	"time"
)

// Router is a wrapper around an HTTP multiplexer (trie.Mux) which retrieves its
// routes from a passed mongo database.
type Router struct {
	mux                   *triemux.Mux
	mongoUrl              string
	mongoDbName           string
	backendConnectTimeout time.Duration
	backendHeaderTimeout  time.Duration
	logger                logger.Logger
}

type Backend struct {
	BackendId  string `bson:"backend_id"`
	BackendURL string `bson:"backend_url"`
}

type Route struct {
	IncomingPath string `bson:"incoming_path"`
	RouteType    string `bson:"route_type"`
	Handler      string `bson:"handler"`
	BackendId    string `bson:"backend_id"`
	RedirectTo   string `bson:"redirect_to"`
	RedirectType string `bson:"redirect_type"`
}

// NewRouter returns a new empty router instance. You will still need to call
// ReloadRoutes() to do the initial route load.
func NewRouter(mongoUrl, mongoDbName, backendConnectTimeout, backendHeaderTimeout, logFileName string) (rt *Router, err error) {
	beConnTimeout, err := time.ParseDuration(backendConnectTimeout)
	if err != nil {
		return nil, err
	}
	beHeaderTimeout, err := time.ParseDuration(backendHeaderTimeout)
	if err != nil {
		return nil, err
	}
	logInfo("router: using backend connect timeout:", beConnTimeout)
	logInfo("router: using backend header timeout:", beHeaderTimeout)

	l, err := logger.New(logFileName)
	if err != nil {
		return nil, err
	}
	logInfo("router: logging errors as JSON to", logFileName)

	rt = &Router{
		mux:                   triemux.NewMux(),
		mongoUrl:              mongoUrl,
		mongoDbName:           mongoDbName,
		backendConnectTimeout: beConnTimeout,
		backendHeaderTimeout:  beHeaderTimeout,
		logger:                l,
	}
	return rt, nil
}

// ServeHTTP delegates responsibility for serving requests to the proxy mux
// instance for this router.
func (rt *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	defer func() {
		if r := recover(); r != nil {
			logWarn("router: recovered from panic in ServeHTTP:", r)
			rt.logger.LogFromClientRequest(map[string]interface{}{"error": fmt.Sprintf("panic: %v", r), "status": 500}, req)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}()

	rt.mux.ServeHTTP(w, req)
}

// ReloadRoutes reloads the routes for this Router instance on the fly. It will
// create a new proxy mux, load applications (backends) and routes into it, and
// then flip the "mux" pointer in the Router.
func (rt *Router) ReloadRoutes() {
	// save a reference to the previous mux in case we have to restore it
	oldmux := rt.mux
	defer func() {
		if r := recover(); r != nil {
			logWarn("router: recovered from panic in ReloadRoutes:", r)
			rt.mux = oldmux
			logInfo("router: original routes have been restored")
		}
	}()

	logDebug("mgo: connecting to", rt.mongoUrl)
	sess, err := mgo.Dial(rt.mongoUrl)
	if err != nil {
		panic(fmt.Sprintln("mgo:", err))
	}
	defer sess.Close()
	sess.SetMode(mgo.Strong, true)

	db := sess.DB(rt.mongoDbName)

	logInfo("router: reloading routes")
	newmux := triemux.NewMux()

	backends := rt.loadBackends(db.C("backends"))
	loadRoutes(db.C("routes"), newmux, backends)

	rt.mux = newmux
	logInfo(fmt.Sprintf("router: reloaded %d routes (checksum: %x)", rt.mux.RouteCount(), rt.mux.RouteChecksum()))
}

// loadBackends is a helper function which loads backends from the
// passed mongo collection, constructs a Handler for each one, and returns
// them in map keyed on the backend_id
func (rt *Router) loadBackends(c *mgo.Collection) (backends map[string]http.Handler) {
	backend := &Backend{}
	backends = make(map[string]http.Handler)

	iter := c.Find(nil).Iter()

	for iter.Next(&backend) {
		backendUrl, err := url.Parse(backend.BackendURL)
		if err != nil {
			logWarn(fmt.Sprintf("router: couldn't parse URL %s for backend %s "+
				"(error: %v), skipping!", backend.BackendURL, backend.BackendId, err))
			continue
		}

		backends[backend.BackendId] = handlers.NewBackendHandler(backendUrl, rt.backendConnectTimeout, rt.backendHeaderTimeout, rt.logger)
	}

	if err := iter.Err(); err != nil {
		panic(err)
	}

	return
}

// loadRoutes is a helper function which loads routes from the passed mongo
// collection and registers them with the passed proxy mux.
func loadRoutes(c *mgo.Collection, mux *triemux.Mux, backends map[string]http.Handler) {
	route := &Route{}

	iter := c.Find(nil).Sort("incoming_path", "route_type").Iter()

	for iter.Next(&route) {
		prefix := (route.RouteType == "prefix")
		suffix := (route.RouteType == "suffix")
		switch route.Handler {
		case "backend":
			handler, ok := backends[route.BackendId]
			if !ok {
				logWarn(fmt.Sprintf("router: found route %+v which references unknown backend "+
					"%s, skipping!", route, route.BackendId))
				continue
			}
			mux.Handle(route.IncomingPath, prefix, suffix, handler)
			logDebug(fmt.Sprintf("router: registered %s (prefix: %v, suffix: %v) for %s",
				route.IncomingPath, prefix, suffix, route.BackendId))
		case "redirect":
			redirectTemporarily := (route.RedirectType == "temporary")
			handler := handlers.NewRedirectHandler(route.IncomingPath, route.RedirectTo, prefix, redirectTemporarily)
			mux.Handle(route.IncomingPath, prefix, suffix, handler)
			logDebug(fmt.Sprintf("router: registered %s (prefix: %v, suffix: %v) -> %s",
				route.IncomingPath, prefix, suffix, route.RedirectTo))
		case "gone":
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusGone)
			})
			mux.Handle(route.IncomingPath, prefix, suffix, handler)
			logDebug(fmt.Sprintf("router: registered %s (prefix: %v, suffix: %v) -> Gone", route.IncomingPath, prefix, suffix))
		case "boom":
			// Special handler so that we can test failure behaviour.
			mux.Handle(route.IncomingPath, prefix, suffix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic("Boom!!!")
			}))
			logDebug(fmt.Sprintf("router: registered %s (prefix: %v, suffix: %v) -> Boom!!!", route.IncomingPath, prefix, suffix))
		default:
			logWarn(fmt.Sprintf("router: found route %+v with unknown handler type "+
				"%s, skipping!", route, route.Handler))
			continue
		}
	}

	if err := iter.Err(); err != nil {
		panic(err)
	}
}

func (rt *Router) RouteStats() (stats map[string]interface{}) {
	stats = make(map[string]interface{})
	stats["count"] = rt.mux.RouteCount()
	stats["checksum"] = fmt.Sprintf("%x", rt.mux.RouteChecksum())
	return
}
