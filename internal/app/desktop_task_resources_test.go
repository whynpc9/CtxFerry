package app

import (
	"context"
	"net"
	"os/exec"
	"testing"
)

func TestDesktopTaskResourcesResolveProbeEvidence(t *testing.T) {
	ctx := context.Background()
	a := App{DataDir: t.TempDir()}
	if _, err := a.Init(ctx); err != nil {
		t.Fatal(err)
	}
	checkout := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.email=t@example.invalid", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init"}} {
		if out, err := exec.Command("git", append([]string{"-C", checkout}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %v: %s", err, out)
		}
	}
	bound, err := a.BindProject(ctx, checkout, "sample")
	if err != nil {
		t.Fatal(err)
	}
	projectID := bound.Project.ID
	if _, err := a.CreateProjectProfile(ctx, projectID, "dev"); err != nil {
		t.Fatal(err)
	}
	resource, err := a.AddResource(ctx, projectID, checkout, "shared", ResourceSpec{
		Identity: "postgres-primary", Kind: "database", Stage: "dev", Purpose: "Test database", Endpoints: []string{"db.example.com:5432"},
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := a.AddTask(ctx, projectID, checkout, "shared", TaskSpec{
		Name: "check-db", Purpose: "Check database", MembershipID: bound.Binding.MembershipID, WorkingDirectory: ".", Program: "check", Argv: []string{},
		ResourceSlots: []TaskResourceSlot{{Name: "db", Kind: "database", Stage: "dev"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.MapTaskResource(ctx, projectID, checkout, "dev", task.ID, "db", resource.ID); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err == nil {
			conn.Close()
		}
	}()
	if _, err := a.SetResourceRoute(ctx, projectID, checkout, resource.ID, listener.Addr().String(), "", ""); err != nil {
		t.Fatal(err)
	}
	evidence, err := a.DesktopTaskResources(ctx, projectID, "dev", task.ID)
	if err != nil || len(evidence) != 1 || evidence[0].Resource.ID != resource.ID || evidence[0].Probe == nil {
		t.Fatalf("mapped Resource evidence: %+v, %v", evidence, err)
	}
	if got, want := evidence[0].Probe.Scope.Target, listener.Addr().String(); got != want {
		t.Fatalf("probe target %q; want %q", got, want)
	}
	observation, err := a.ProbeDesktopResource(ctx, *evidence[0].Probe)
	if err != nil || observation.Status != "reachable" {
		t.Fatalf("probe: %+v, %v", observation, err)
	}
	<-done
	evidence, err = a.DesktopTaskResources(ctx, projectID, "dev", task.ID)
	if err != nil || len(evidence) != 1 || evidence[0].Latest == nil {
		t.Fatalf("latest observation missing: %+v, %v", evidence, err)
	}
	latest := evidence[0].Latest
	if latest.ObservedAt == "" || latest.Scope.Target != listener.Addr().String() || latest.Status != "reachable" {
		t.Fatalf("wrong observation: %+v", latest)
	}
}
