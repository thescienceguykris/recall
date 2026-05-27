package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestDeleteJobRemovesJobAndArtefacts(t *testing.T) {
	db := New(filepath.Join(t.TempDir(), "db.json"))
	if err := db.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	job := Job{ID: "job1", SourceURL: "https://example.com", Created: time.Now(), Updated: time.Now()}
	if err := db.UpsertJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if err := db.AddArtefact(context.Background(), Artefact{ID: "note1", JobID: "job1", Name: "note.md"}); err != nil {
		t.Fatal(err)
	}
	if err := db.AddArtefact(context.Background(), Artefact{ID: "other", JobID: "job2", Name: "other.md"}); err != nil {
		t.Fatal(err)
	}

	deleted, err := db.DeleteJob(context.Background(), "job1")
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("expected job to be deleted")
	}
	if _, ok, err := db.GetJob(context.Background(), "job1"); err != nil || ok {
		t.Fatalf("job still exists: ok=%v err=%v", ok, err)
	}
	if _, ok, err := db.GetArtefact(context.Background(), "note1"); err != nil || ok {
		t.Fatalf("artefact still exists: ok=%v err=%v", ok, err)
	}
	if _, ok, err := db.GetArtefact(context.Background(), "other"); err != nil || !ok {
		t.Fatalf("unrelated artefact missing: ok=%v err=%v", ok, err)
	}
}

func TestJSONStoreScopesRecordsByUser(t *testing.T) {
	db := New(filepath.Join(t.TempDir(), "db.json"))
	if err := db.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	user1 := WithUserID(context.Background(), LocalUserID)
	user2 := WithUserID(context.Background(), "11111111-1111-4111-8111-111111111111")

	if err := db.UpsertJob(user1, Job{ID: "job1", SourceURL: "https://example.com/1", Created: time.Now(), Updated: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertJob(user2, Job{ID: "job2", SourceURL: "https://example.com/2", Created: time.Now(), Updated: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := db.AddArtefact(user2, Artefact{ID: "note2", JobID: "job2", Name: "note.md"}); err != nil {
		t.Fatal(err)
	}

	jobs, err := db.ListJobs(user1)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != "job1" {
		t.Fatalf("user1 jobs = %+v", jobs)
	}
	if _, ok, err := db.GetJob(user1, "job2"); err != nil || ok {
		t.Fatalf("user1 can see user2 job: ok=%v err=%v", ok, err)
	}
	if _, ok, err := db.GetArtefact(user1, "note2"); err != nil || ok {
		t.Fatalf("user1 can see user2 artefact: ok=%v err=%v", ok, err)
	}
}
