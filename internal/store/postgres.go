package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"time"

	"github.com/lib/pq"
)

type Postgres struct {
	db *sql.DB
}

func NewPostgres(databaseURL string) (*Postgres, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, err
	}
	return &Postgres{db: db}, nil
}

func (p *Postgres) Close() error {
	return p.db.Close()
}

func (p *Postgres) Init(ctx context.Context) error {
	if err := p.db.PingContext(ctx); err != nil {
		return err
	}
	for _, stmt := range postgresSchema {
		if _, err := p.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	_, err := p.EnsureUser(ctx, User{
		ID:              LocalUserID,
		Provider:        "local",
		ProviderSubject: LocalUserID,
		Name:            "Local",
	})
	return err
}

func (p *Postgres) EnsureUser(ctx context.Context, user User) (User, error) {
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	if user.ID == "" {
		id, err := newUUID()
		if err != nil {
			return User{}, err
		}
		user.ID = id
	}
	if user.Provider == "" {
		user.Provider = "local"
	}
	if user.ProviderSubject == "" {
		user.ProviderSubject = user.ID
	}
	row := p.db.QueryRowContext(ctx, `
		insert into users (id, provider, provider_subject, email, name, picture_url)
		values ($1, $2, $3, $4, $5, $6)
		on conflict (provider, provider_subject) do update set
			email = excluded.email,
			name = excluded.name,
			picture_url = excluded.picture_url,
			updated_at = now()
		returning id, provider, provider_subject, coalesce(email, ''), coalesce(name, ''), coalesce(picture_url, ''), created_at, updated_at
	`, user.ID, user.Provider, user.ProviderSubject, nullString(user.Email), nullString(user.Name), nullString(user.PictureURL))
	if err := row.Scan(&user.ID, &user.Provider, &user.ProviderSubject, &user.Email, &user.Name, &user.PictureURL, &user.Created, &user.Updated); err != nil {
		return User{}, err
	}
	return user, nil
}

func (p *Postgres) UpsertJob(ctx context.Context, job Job) error {
	return p.withUserTx(ctx, func(tx *sql.Tx) error {
		if job.OwnerID == "" {
			job.OwnerID = UserIDFromContext(ctx)
		}
		_, err := tx.ExecContext(ctx, `
			insert into jobs (id, owner_id, source_url, title, tags, status, stage, message, error, note_id, transcript_id, created_at, updated_at)
			values ($1, $2, $3, $4, $5, $6, $7, $8, nullif($9, ''), nullif($10, ''), nullif($11, ''), $12, $13)
			on conflict (id) do update set
				source_url = excluded.source_url,
				title = excluded.title,
				tags = excluded.tags,
				status = excluded.status,
				stage = excluded.stage,
				message = excluded.message,
				error = excluded.error,
				note_id = excluded.note_id,
				transcript_id = excluded.transcript_id,
				updated_at = excluded.updated_at
		`, job.ID, job.OwnerID, job.SourceURL, job.Title, pq.Array(job.Tags), job.Status, job.Stage, job.Message, job.Error, job.NoteID, job.TranscriptID, job.Created, job.Updated)
		return err
	})
}

func (p *Postgres) AddArtefact(ctx context.Context, artefact Artefact) error {
	return p.withUserTx(ctx, func(tx *sql.Tx) error {
		if artefact.OwnerID == "" {
			artefact.OwnerID = UserIDFromContext(ctx)
		}
		_, err := tx.ExecContext(ctx, `
			insert into artefacts (id, owner_id, job_id, name, kind, media_type, content, created_at, updated_at)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`, artefact.ID, artefact.OwnerID, artefact.JobID, artefact.Name, artefact.Kind, artefact.MediaType, artefact.Content, artefact.Created, nullTime(artefact.Updated))
		return err
	})
}

func (p *Postgres) UpsertArtefact(ctx context.Context, artefact Artefact) error {
	return p.withUserTx(ctx, func(tx *sql.Tx) error {
		if artefact.OwnerID == "" {
			artefact.OwnerID = UserIDFromContext(ctx)
		}
		_, err := tx.ExecContext(ctx, `
			insert into artefacts (id, owner_id, job_id, name, kind, media_type, content, created_at, updated_at)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			on conflict (id) do update set
				name = excluded.name,
				kind = excluded.kind,
				media_type = excluded.media_type,
				content = excluded.content,
				updated_at = excluded.updated_at
		`, artefact.ID, artefact.OwnerID, artefact.JobID, artefact.Name, artefact.Kind, artefact.MediaType, artefact.Content, artefact.Created, nullTime(artefact.Updated))
		return err
	})
}

func (p *Postgres) GetJob(ctx context.Context, id string) (Job, bool, error) {
	var job Job
	err := p.withUserTx(ctx, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `
			select id, owner_id, source_url, title, tags, status, stage, message, coalesce(error, ''), coalesce(note_id, ''), coalesce(transcript_id, ''), created_at, updated_at
			from jobs
			where id = $1
		`, id)
		return row.Scan(&job.ID, &job.OwnerID, &job.SourceURL, &job.Title, pq.Array(&job.Tags), &job.Status, &job.Stage, &job.Message, &job.Error, &job.NoteID, &job.TranscriptID, &job.Created, &job.Updated)
	})
	if err == sql.ErrNoRows {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, err
	}
	return job, true, nil
}

func (p *Postgres) ListJobs(ctx context.Context) ([]Job, error) {
	var jobs []Job
	err := p.withUserTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			select id, owner_id, source_url, title, tags, status, stage, message, coalesce(error, ''), coalesce(note_id, ''), coalesce(transcript_id, ''), created_at, updated_at
			from jobs
			order by created_at desc
		`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var job Job
			if err := rows.Scan(&job.ID, &job.OwnerID, &job.SourceURL, &job.Title, pq.Array(&job.Tags), &job.Status, &job.Stage, &job.Message, &job.Error, &job.NoteID, &job.TranscriptID, &job.Created, &job.Updated); err != nil {
				return err
			}
			jobs = append(jobs, job)
		}
		return rows.Err()
	})
	return jobs, err
}

func (p *Postgres) GetArtefact(ctx context.Context, id string) (Artefact, bool, error) {
	var artefact Artefact
	err := p.withUserTx(ctx, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `
			select id, owner_id, job_id, name, kind, media_type, content, created_at, updated_at
			from artefacts
			where id = $1
		`, id)
		var updated sql.NullTime
		if err := row.Scan(&artefact.ID, &artefact.OwnerID, &artefact.JobID, &artefact.Name, &artefact.Kind, &artefact.MediaType, &artefact.Content, &artefact.Created, &updated); err != nil {
			return err
		}
		if updated.Valid {
			artefact.Updated = updated.Time
		}
		return nil
	})
	if err == sql.ErrNoRows {
		return Artefact{}, false, nil
	}
	if err != nil {
		return Artefact{}, false, err
	}
	return artefact, true, nil
}

func (p *Postgres) DeleteJob(ctx context.Context, id string) (bool, error) {
	var rows int64
	err := p.withUserTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `delete from jobs where id = $1`, id)
		if err != nil {
			return err
		}
		rows, err = result.RowsAffected()
		return err
	})
	return rows > 0, err
}

func (p *Postgres) withUserTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `select set_config('app.current_user_id', $1, true)`, UserIDFromContext(ctx)); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func nullString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func nullTime(value time.Time) sql.NullTime {
	return sql.NullTime{Time: value, Valid: !value.IsZero()}
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[0:4]) + "-" +
		hex.EncodeToString(b[4:6]) + "-" +
		hex.EncodeToString(b[6:8]) + "-" +
		hex.EncodeToString(b[8:10]) + "-" +
		hex.EncodeToString(b[10:16]), nil
}

var postgresSchema = []string{
	`create table if not exists users (
		id uuid primary key,
		provider text not null,
		provider_subject text not null,
		email text,
		name text,
		picture_url text,
		created_at timestamptz not null default now(),
		updated_at timestamptz not null default now(),
		unique (provider, provider_subject)
	)`,
	`create table if not exists jobs (
		id text primary key,
		owner_id uuid not null references users(id) on delete cascade,
		source_url text not null,
		title text not null default '',
		tags text[] not null default '{}',
		status text not null,
		stage text not null,
		message text not null,
		error text,
		note_id text,
		transcript_id text,
		created_at timestamptz not null,
		updated_at timestamptz not null
	)`,
	`create table if not exists artefacts (
		id text primary key,
		owner_id uuid not null references users(id) on delete cascade,
		job_id text not null references jobs(id) on delete cascade,
		name text not null,
		kind text not null,
		media_type text not null,
		content text not null,
		created_at timestamptz not null,
		updated_at timestamptz
	)`,
	`alter table jobs enable row level security`,
	`alter table jobs force row level security`,
	`alter table artefacts enable row level security`,
	`alter table artefacts force row level security`,
	`drop policy if exists jobs_owner_policy on jobs`,
	`create policy jobs_owner_policy on jobs
		using (owner_id = nullif(current_setting('app.current_user_id', true), '')::uuid)
		with check (owner_id = nullif(current_setting('app.current_user_id', true), '')::uuid)`,
	`drop policy if exists artefacts_owner_policy on artefacts`,
	`create policy artefacts_owner_policy on artefacts
		using (owner_id = nullif(current_setting('app.current_user_id', true), '')::uuid)
		with check (owner_id = nullif(current_setting('app.current_user_id', true), '')::uuid)`,
	`create index if not exists jobs_owner_created_idx on jobs (owner_id, created_at desc)`,
	`create index if not exists artefacts_owner_job_idx on artefacts (owner_id, job_id)`,
}
