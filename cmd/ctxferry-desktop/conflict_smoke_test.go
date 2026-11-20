//go:build ctxferry_test

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
)

func TestConflictFromQueueToSharedRevision(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	if out, err := exec.Command("git", "init", "--bare", origin).CombinedOutput(); err != nil {
		t.Fatalf("initializing fixture remote: %v: %s", err, out)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/fixture/conflict" || r.Header.Get("Authorization") != "Bearer fixture-token" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"id":42,"full_name":"fixture/conflict","private":true}`)
	}))
	t.Cleanup(server.Close)
	t.Setenv("CTXFERRY_TEST_API", server.URL)
	t.Setenv("CTXFERRY_TEST_REMOTE", origin)
	t.Setenv("GH_TOKEN", "fixture-token")

	require := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	const remoteURL = "https://github.com/fixture/conflict.git"
	const branch = "ctxferry-v1"
	remote := app.App{DataDir: filepath.Join(root, "remote")}
	local := app.App{DataDir: filepath.Join(root, "local")}
	remoteCheckout, localCheckout := filepath.Join(root, "peer"), filepath.Join(root, "worktree")
	require(os.Mkdir(remoteCheckout, 0700))
	require(os.Mkdir(localCheckout, 0700))
	_, err := remote.Init(ctx)
	require(err)
	_, err = remote.BindProject(ctx, remoteCheckout, "fixture")
	require(err)
	put := func(device app.App, checkout, id, revision, body string) *app.ContextDoc {
		t.Helper()
		r, err := device.PutContext(ctx, "", checkout, app.PutContext{
			ID: id, IfRevision: revision, Body: body, Title: "测试环境说明",
			Purpose: "notes", Source: "fixture", Storage: "shared", Disclosure: "full",
		})
		require(err)
		return r.Document
	}
	base := put(remote, remoteCheckout, "", "", "共同正文")
	_, err = remote.EnableSync(ctx, remoteURL, branch)
	require(err)
	preview, err := remote.PreviewSync(ctx)
	require(err)
	_, err = remote.InitSync(ctx, preview.PreviewID)
	require(err)
	_, err = local.Join(ctx, remoteURL, branch, nil, true)
	require(err)
	_, err = local.BindProjectWith(ctx, localCheckout, "", "fixture", "", nil)
	require(err)
	put(local, localCheckout, base.ID, base.Revision, "本机正文")
	peer := put(remote, remoteCheckout, base.ID, base.Revision, "远端正文")
	_, err = remote.Sync(ctx)
	require(err)
	_, err = local.Sync(ctx)
	require(err)

	updates := make(chan func(), 4)
	snapshot, err := local.ReadDesktopSnapshot(ctx)
	require(err)
	d := desktop{ctx: ctx, core: local, data: snapshot, page: "设备总览", update: func(f func()) { updates <- f }}
	window := ui.NewTester(d.view, 1280, 1100)
	applyUpdate := func() {
		t.Helper()
		select {
		case f := <-updates:
			f()
			window.Frame()
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for desktop update")
		}
	}
	click := func(label string) {
		t.Helper()
		require(window.Click(label))
	}
	visible := func(labels ...string) {
		t.Helper()
		for _, label := range labels {
			if _, ok := window.Access(label); !ok {
				t.Errorf("missing %q", label)
			}
		}
	}

	click("查看冲突队列")
	visible("测试环境说明")
	click("比较版本 · " + base.ID)
	applyUpdate()
	click("返回同步队列")
	if !window.Focused("比较版本 · " + base.ID) {
		t.Fatal("returning to the queue lost focus on the triggering record")
	}
	click("比较版本 · " + base.ID)
	applyUpdate()
	click("设备总览")
	click("查看冲突队列")
	if d.conflict != nil {
		t.Fatal("opening the queue from Overview reopened the prior comparison")
	}
	click("比较版本 · " + base.ID)
	applyUpdate()
	visible("共同基线", "本机版本", "远端版本", "共同正文", "本机正文", "远端正文", peer.DeviceID, "+ 新增", "− 移除")
	if d.conflict.canConfirm() || d.conflict.choice != "" {
		t.Fatal("opened a conflict with a preselected result")
	}
	click("采用远端")
	click("生成本机修订…")
	click("取消")
	if _, err := local.Conflict(ctx, base.ID); err != nil {
		t.Fatalf("cancel changed conflict: %v", err)
	}

	click("编辑新版本")
	click("编辑结果正文")
	window.Command("selectAll")
	const newBody = "  合并正文\n保留空白。\n"
	window.Type(newBody)
	if body := d.conflict.draft.input.Body; body != newBody {
		t.Fatalf("editor lost content: %q", body)
	}

	peer = put(remote, remoteCheckout, base.ID, peer.Revision, "远端再次编辑")
	_, err = remote.Sync(ctx)
	require(err)
	_, err = local.Sync(ctx)
	require(err)
	click("生成本机修订…")
	click("确认生成")
	applyUpdate()
	if !d.conflict.review || !strings.Contains(d.conflict.err, "已变化") || d.conflict.canConfirm() {
		t.Fatalf("stale comparison was not invalidated: %+v", d.conflict)
	}
	click("刷新比较")
	applyUpdate()
	visible("远端再次编辑")
	if d.conflict.choice != "" || d.conflict.draft.input.Body != newBody || d.conflict.canConfirm() {
		t.Fatal("refresh did not require a new choice and preserve the draft")
	}

	click("编辑新版本")
	// A new decision on the disclosure range is required after versions advance.
	click("选择 Agent 披露范围")
	click("full")
	click("生成本机修订…")
	click("确认生成")
	applyUpdate()
	applyUpdate()
	visible(conflictReceipt)
	resolved, err := local.QueryContext(ctx, "", localCheckout, "get", base.ID, "")
	require(err)
	doc := resolved.Document
	if doc.Body == nil || *doc.Body != newBody || doc.PreviousRevision != peer.Revision ||
		doc.Revision == peer.Revision || doc.DeviceID != snapshot.Device.ID {
		t.Fatalf("incorrect resolve result: %+v", doc)
	}
	visible("记录 ID "+base.ID, "本机修订 "+doc.Revision)
	if len(d.data.Sync.Conflicts) != 0 || len(d.data.PendingIDs) != 1 || d.data.PendingIDs[0] != base.ID || d.data.Sync.Head != d.conflict.detail.Head {
		t.Fatalf("write should leave one pending resolution at the inspected head: %+v", d.data)
	}
	click("返回同步队列")
	visible("已生成本机修订 · 待发布", "测试环境说明", "记录 ID "+base.ID, "本机修订 "+doc.Revision)
	_, err = local.Sync(ctx)
	require(err)
	_, err = remote.Sync(ctx)
	require(err)
	reloaded, err := remote.QueryContext(ctx, "", remoteCheckout, "get", base.ID, "")
	require(err)
	if reloaded.Document.Revision != doc.Revision || *reloaded.Document.Body != newBody {
		t.Fatalf("peer did not receive resolved revision: %+v", reloaded.Document)
	}
}
