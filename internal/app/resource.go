package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/whynpc9/CtxFerry/internal/store"
)

type ResourceSpec struct {
	Identity  string   `json:"identity"`
	Kind      string   `json:"kind"`
	Stage     string   `json:"stage"`
	Purpose   string   `json:"purpose"`
	Endpoints []string `json:"endpoints"`
	Network   string   `json:"network,omitempty"`
	Platforms []string `json:"platforms"`
}

type Resource struct {
	ResourceSpec
	ID        string `json:"id"`
	Scope     Scope  `json:"scope"`
	Policy    Policy `json:"policy"`
	Revision  string `json:"revision"`
	UpdatedAt string `json:"updatedAt"`
}

type ResourcePurpose struct {
	ID       string `json:"id"`
	Identity string `json:"identity"`
	Purpose  string `json:"purpose"`
}

type ResourceList struct {
	Project   Project    `json:"project"`
	Resources []Resource `json:"resources"`
}

type ResourceRoute struct {
	Status   string `json:"status"`
	Source   string `json:"source,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	Proxy    string `json:"proxy,omitempty"`
	Network  string `json:"network,omitempty"`
}

type ResourceDetail struct {
	Resource     Resource      `json:"resource"`
	Environment  Environment   `json:"environment"`
	Route        ResourceRoute `json:"route"`
	Observations []Observation `json:"observations"`
}

type ObservationScope struct {
	Kind             string `json:"kind"`
	Target           string `json:"target"`
	Network          string `json:"network,omitempty"`
	OS               string `json:"os"`
	Arch             string `json:"arch"`
	ResourceRevision string `json:"resourceRevision"`
}

type Observation struct {
	ID              string           `json:"id"`
	ResourceID      string           `json:"resourceId"`
	DeviceID        string           `json:"deviceId"`
	Tool            string           `json:"tool"`
	ObservedAt      string           `json:"observedAt"`
	Scope           ObservationScope `json:"scope"`
	Status          string           `json:"status"`
	ServiceVerified bool             `json:"serviceVerified"`
	EnvironmentKind string           `json:"environmentKind,omitempty"`
}

var resourceToken = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
var platformToken = regexp.MustCompile(`^[a-z0-9]+/[a-z0-9]+$`)
var hostLabel = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

func validateEndpoint(endpoint string, shared bool) error {
	host, port, err := net.SplitHostPort(endpoint)
	number, portErr := strconv.ParseUint(port, 10, 16)
	if err != nil || portErr != nil || number == 0 || strconv.FormatUint(number, 10) != port || host == "" {
		return Fail("invalid-endpoint", "use a host and numeric port, such as db.example.com:5432")
	}
	if addr, err := netip.ParseAddr(strings.TrimSuffix(host, ".")); err == nil {
		addr = addr.Unmap()
		if addr.IsUnspecified() || addr.IsMulticast() {
			return Fail("invalid-endpoint", "endpoint must identify one TCP host")
		}
		if shared && (addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.Zone() != "") {
			return Fail("invalid-endpoint", "device-local endpoints belong in local route facts")
		}
		return nil
	}
	name := strings.ToLower(strings.TrimSuffix(host, "."))
	if len(name) > 253 {
		return Fail("invalid-endpoint", "endpoint host is invalid")
	}
	for _, label := range strings.Split(name, ".") {
		if !hostLabel.MatchString(label) {
			return Fail("invalid-endpoint", "endpoint host is invalid")
		}
	}
	if shared && (!strings.Contains(name, ".") || strings.HasSuffix(name, ".localhost") || strings.HasSuffix(name, ".local")) {
		return Fail("invalid-endpoint", "shared endpoints need a cross-device hostname or IP; use local route facts for device-local endpoints")
	}
	return nil
}

func validateResourceSpec(spec ResourceSpec, shared bool) error {
	for _, value := range []string{spec.Identity, spec.Kind, spec.Stage, spec.Purpose} {
		if strings.TrimSpace(value) == "" || len(value) > 1024 || !utf8.ValidString(value) || strings.ContainsFunc(value, unicode.IsControl) {
			return Fail("invalid-resource", "identity, kind, stage and purpose must be nonempty plain text of at most 1024 bytes each")
		}
	}
	if spec.Network != "" && !resourceToken.MatchString(spec.Network) {
		return Fail("invalid-resource", "network must be a lowercase label")
	}
	if len(spec.Endpoints) > 32 || len(spec.Platforms) > 32 {
		return Fail("invalid-resource", "set at most 32 endpoints and 32 platform conditions")
	}
	endpoints := map[string]bool{}
	for _, endpoint := range spec.Endpoints {
		if len(endpoint) > 300 || endpoints[endpoint] {
			return Fail("invalid-resource", "endpoints must be distinct and at most 300 bytes each")
		}
		if err := validateEndpoint(endpoint, shared); err != nil {
			return err
		}
		endpoints[endpoint] = true
	}
	platforms := map[string]bool{}
	for _, platform := range spec.Platforms {
		if len(platform) > 63 || !platformToken.MatchString(platform) || platforms[platform] {
			return Fail("invalid-resource", "platform conditions must be distinct os/arch pairs")
		}
		platforms[platform] = true
	}
	return nil
}

func resourceFromRecord(r remoteRecord) Resource {
	return Resource{ResourceSpec: r.Resource, ID: r.ID, Scope: r.Scope, Policy: r.Policy, Revision: r.Revision, UpdatedAt: r.Provenance.UpdatedAt}
}

func resourceRecord(raw []byte) (remoteRecord, error) {
	r, err := decodeRecord(raw)
	if err != nil {
		return r, err
	}
	if r.Kind != "resource" || r.Scope.Kind != "project" || !validPolicy(r.Policy) || r.Policy.AgentDisclosure != "full" {
		return r, Fail("invalid-resource", "invalid Resource record scope or policy")
	}
	return r, validateResourceSpec(r.Resource, r.Policy.Storage == "shared")
}

func (a App) AddResource(ctx context.Context, selector, cwd, storage string, spec ResourceSpec) (Resource, error) {
	var out Resource
	if storage != "shared" && storage != "local-only" {
		return out, Fail("invalid-policy", "choose storage local-only or shared")
	}
	shared := storage == "shared"
	if err := validateResourceSpec(spec, shared); err != nil {
		return out, err
	}
	if spec.Endpoints == nil {
		spec.Endpoints = []string{}
	}
	if spec.Platforms == nil {
		spec.Platforms = []string{}
	}
	if shared {
		release, err := a.syncOwner(ctx)
		if err != nil {
			return out, err
		}
		defer release()
	}
	db, project, err := a.openSubscribedProject(ctx, selector, cwd)
	if err != nil {
		return out, err
	}
	defer db.Close()
	err = a.mutateCatalog(ctx, db, func(c *sql.Conn) (bool, error) {
		env := publishedEnvelope{SchemaVersion: 1, Kind: "resource", Scope: Scope{"project", project.ID}, Policy: Policy{storage, "full"}, Payload: spec}
		for _, id := range []*string{&env.ID, &env.Revision, &env.OperationID} {
			value, err := newID()
			if err != nil {
				return false, err
			}
			*id = value
		}
		env.Provenance.UpdatedAt = now()
		if err := c.QueryRowContext(ctx, "SELECT workspace.id, device.id FROM workspace CROSS JOIN device").Scan(&env.WorkspaceID, &env.Provenance.DeviceID); err != nil {
			return false, err
		}
		raw, err := json.Marshal(env)
		if err != nil {
			return false, err
		}
		if _, err = c.ExecContext(ctx, "INSERT INTO records VALUES(?,?,'resource',?)", env.ID, project.ID, string(raw)); err != nil {
			return false, err
		}
		out = resourceFromRecord(remoteRecord{publishedEnvelope: env, Resource: spec})
		return shared, nil
	})
	return out, err
}

const liveResources = ` FROM records WHERE kind='resource' AND project_id=? AND coalesce(json_extract(metadata,'$.deleted'),0)=0`

func (a App) ListResources(ctx context.Context, selector, cwd string) (ResourceList, error) {
	out := ResourceList{Resources: []Resource{}}
	db, project, err := a.openSubscribedProject(ctx, selector, cwd)
	if err != nil {
		return out, err
	}
	defer db.Close()
	return listProjectResources(ctx, db, project)
}

func listProjectResources(ctx context.Context, db taskReader, project Project) (ResourceList, error) {
	out := ResourceList{Project: project, Resources: []Resource{}}
	rows, err := db.QueryContext(ctx, "SELECT metadata"+liveResources+" ORDER BY id", project.ID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return out, err
		}
		r, err := resourceRecord(raw)
		if err != nil {
			return out, err
		}
		out.Resources = append(out.Resources, resourceFromRecord(r))
	}
	return out, rows.Err()
}

type resourceReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadResource(ctx context.Context, db resourceReader, projectID, id string) (Resource, error) {
	var raw []byte
	err := db.QueryRowContext(ctx, "SELECT metadata"+liveResources+" AND id=?", projectID, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Resource{}, Fail("resource-not-found", "Resource not found in this Project")
	}
	if err != nil {
		return Resource{}, err
	}
	r, err := resourceRecord(raw)
	return resourceFromRecord(r), err
}

func (a App) SetResourceRoute(ctx context.Context, selector, cwd, id, endpoint, proxy, network string) (ResourceDetail, error) {
	var out ResourceDetail
	if network != "" && !resourceToken.MatchString(network) {
		return out, Fail("usage", "network must be a lowercase label")
	}
	for _, address := range []string{endpoint, proxy} {
		if address != "" {
			if err := validateEndpoint(address, false); err != nil {
				return out, err
			}
		}
	}
	db, project, err := a.openSubscribedProject(ctx, selector, cwd)
	if err != nil {
		return out, err
	}
	defer db.Close()
	err = store.Write(ctx, db, func(c *sql.Conn) error {
		if _, err := loadResource(ctx, c, project.ID, id); err != nil {
			return err
		}
		if endpoint == "" && proxy == "" && network == "" {
			_, err := c.ExecContext(ctx, "DELETE FROM resource_routes WHERE resource_id=? AND device_id=(SELECT id FROM device)", id)
			return err
		}
		_, err := c.ExecContext(ctx, `INSERT INTO resource_routes SELECT ?,id,?,?,? FROM device WHERE true
			ON CONFLICT(resource_id,device_id) DO UPDATE SET endpoint=excluded.endpoint,proxy=excluded.proxy,network=excluded.network`, id, endpoint, proxy, network)
		return err
	})
	if err != nil {
		return out, err
	}
	return resourceDetail(ctx, db, project.ID, cwd, id)
}

func resourceDetail(ctx context.Context, db resourceReader, projectID, cwd, id string) (ResourceDetail, error) {
	out := ResourceDetail{
		Environment:  Environment{OS: runtime.GOOS, Arch: runtime.GOARCH, CWD: cwd},
		Observations: []Observation{},
	}
	resource, err := loadResource(ctx, db, projectID, id)
	if err != nil {
		return out, err
	}
	out.Resource = resource
	err = db.QueryRowContext(ctx, `SELECT d.id,coalesce(r.endpoint,''),coalesce(r.proxy,''),coalesce(r.network,'')
		FROM device d LEFT JOIN resource_routes r ON r.device_id=d.id AND r.resource_id=?`, id).Scan(
		&out.Environment.DeviceID, &out.Route.Endpoint, &out.Route.Proxy, &out.Route.Network)
	if err != nil {
		return out, err
	}
	out.Route = resolveResourceRoute(resource.ResourceSpec, out.Environment, out.Route)
	return out, nil
}

func resolveResourceRoute(spec ResourceSpec, env Environment, local ResourceRoute) ResourceRoute {
	if len(spec.Platforms) > 0 {
		matches := false
		for _, platform := range spec.Platforms {
			matches = matches || platform == env.OS+"/"+env.Arch
		}
		if !matches {
			return ResourceRoute{Status: "platform-unsupported"}
		}
	}
	if spec.Network != "" && spec.Network != local.Network {
		return ResourceRoute{Status: "network-mismatch", Network: local.Network}
	}
	if local.Endpoint != "" {
		local.Source = "device-local"
	} else {
		switch len(spec.Endpoints) {
		case 0:
			return ResourceRoute{Status: "route-missing", Network: local.Network}
		case 1:
			local.Endpoint = spec.Endpoints[0]
			local.Source = "resource"
		default:
			return ResourceRoute{Status: "route-ambiguous", Network: local.Network}
		}
	}
	if local.Proxy != "" {
		local.Source = "device-local"
	}
	local.Status = "resolved"
	return local
}

func (a App) InspectResource(ctx context.Context, selector, cwd, id string) (ResourceDetail, error) {
	db, project, err := a.openSubscribedProject(ctx, selector, cwd)
	if err != nil {
		return ResourceDetail{}, err
	}
	defer db.Close()
	var out ResourceDetail
	err = store.Write(ctx, db, func(c *sql.Conn) error {
		var err error
		out, err = resourceDetail(ctx, c, project.ID, cwd, id)
		if err != nil {
			return err
		}
		rows, err := c.QueryContext(ctx, `SELECT data FROM resource_observations WHERE resource_id=? AND device_id=? ORDER BY seq DESC`, id, out.Environment.DeviceID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			if err = rows.Scan(&raw); err != nil {
				return err
			}
			var o Observation
			if err = json.Unmarshal(raw, &o); err != nil {
				return err
			}
			out.Observations = append(out.Observations, o)
		}
		return rows.Err()
	})
	return out, err
}

const resourceCheckTimeout = 2 * time.Second

func (a App) CheckResource(ctx context.Context, selector, cwd, id string) (Observation, error) {
	return a.checkResource(ctx, selector, cwd, id, nil)
}

func (a App) checkResource(ctx context.Context, selector, cwd, id string, expected *DesktopResourceProbe) (Observation, error) {
	var out Observation
	db, project, err := a.openSubscribedProject(ctx, selector, cwd)
	if err != nil {
		return out, err
	}
	defer db.Close()
	detail, err := resourceDetail(ctx, db, project.ID, cwd, id)
	if err != nil {
		return out, err
	}
	if detail.Route.Status != "resolved" {
		return out, Fail(detail.Route.Status, "no unambiguous TCP route for this execution environment")
	}
	scope := resourceProbeScope(detail)
	if expected != nil && (detail.Environment.DeviceID != expected.DeviceID || scope != expected.Scope) {
		return out, Fail("selection-changed", "the TCP target or execution environment changed; review the new route")
	}
	obsID, err := newID()
	if err != nil {
		return out, err
	}
	dialer := net.Dialer{Timeout: resourceCheckTimeout}
	conn, dialErr := dialer.DialContext(ctx, "tcp", scope.Target)
	status := "unreachable"
	if dialErr == nil {
		conn.Close()
		status = "reachable"
	}
	out = Observation{ID: obsID, ResourceID: id, DeviceID: detail.Environment.DeviceID, Tool: "tcp-connect",
		ObservedAt: now(), Scope: scope, Status: status, EnvironmentKind: localDesktopEnvironment()}
	raw, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	// Preserve the outcome even if cancellation interrupted the network attempt.
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err = db.ExecContext(recordCtx, "INSERT INTO resource_observations(resource_id,device_id,data) VALUES(?,?,?)", id, out.DeviceID, string(raw))
	return out, err
}

func resourceProbeScope(detail ResourceDetail) ObservationScope {
	scope := ObservationScope{Kind: "service-endpoint", Target: detail.Route.Endpoint, Network: detail.Route.Network, OS: detail.Environment.OS, Arch: detail.Environment.Arch, ResourceRevision: detail.Resource.Revision}
	if detail.Route.Proxy != "" {
		scope.Kind = "proxy-endpoint"
		scope.Target = detail.Route.Proxy
	}
	return scope
}
