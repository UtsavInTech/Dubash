package speaker

import (
 "context"
 "embed"
 "encoding/json"
 "fmt"
 "io/fs"
 "slices"
 "strconv"
 "strings"
 "time"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
)
//go:embed migrations/*.sql
var migrations embed.FS
type PGStore struct{db *pgxpool.Pool}
func OpenPG(ctx context.Context,url string)(*PGStore,error){db,e:=pgxpool.New(ctx,url);if e!=nil{return nil,e};s:=&PGStore{db};if e=s.migrate(ctx);e!=nil{db.Close();return nil,e};return s,nil}
func(s *PGStore)migrate(ctx context.Context)error{conn,e:=s.db.Acquire(ctx);if e!=nil{return e};defer conn.Release();if _,e=conn.Exec(ctx,`SELECT pg_advisory_lock(9261)`);e!=nil{return e};defer conn.Exec(context.WithoutCancel(ctx),`SELECT pg_advisory_unlock(9261)`);if _,e:=conn.Exec(ctx,`CREATE TABLE IF NOT EXISTS speaker_schema_migrations(version int PRIMARY KEY,applied_at timestamptz NOT NULL DEFAULT now())`);e!=nil{return e};names,e:=fs.Glob(migrations,"migrations/*.sql");if e!=nil{return e};slices.Sort(names);for _,n:=range names{v,e:=strconv.Atoi(strings.SplitN(strings.TrimPrefix(n,"migrations/"),"_",2)[0]);if e!=nil{return e};var done bool;if e=conn.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM speaker_schema_migrations WHERE version=$1)`,v).Scan(&done);e!=nil{return e};if done{continue};b,_:=migrations.ReadFile(n);tx,e:=conn.Begin(ctx);if e!=nil{return e};if _,e=tx.Exec(ctx,string(b));e==nil{_,e=tx.Exec(ctx,`INSERT INTO speaker_schema_migrations(version) VALUES($1)`,v)};if e!=nil{tx.Rollback(ctx);return fmt.Errorf("speaker migration: %w",e)};if e=tx.Commit(ctx);e!=nil{return e}};return nil}
func(s *PGStore)Close()error{s.db.Close();return nil}
func(s *PGStore)SaveProfile(ctx context.Context,p Profile)(Profile,error){now:=time.Now().UTC();b,_:=json.Marshal(p.Languages);e:=s.db.QueryRow(ctx,`INSERT INTO speaker_profiles(id,display_name,languages,style,notes,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$6) ON CONFLICT(id) DO UPDATE SET display_name=excluded.display_name,languages=excluded.languages,style=excluded.style,notes=excluded.notes,updated_at=excluded.updated_at RETURNING created_at,updated_at`,p.ID,p.DisplayName,b,p.Style,p.Notes,now).Scan(&p.CreatedAt,&p.UpdatedAt);return p,e}
func scanProfile(row pgx.Row)(Profile,error){var p Profile;var b []byte;e:=row.Scan(&p.ID,&p.DisplayName,&b,&p.Style,&p.Notes,&p.CreatedAt,&p.UpdatedAt);json.Unmarshal(b,&p.Languages);return p,e}
const profileCols=`id,display_name,languages,style,notes,created_at,updated_at`
func(s *PGStore)Profiles(ctx context.Context)([]Profile,error){rows,e:=s.db.Query(ctx,`SELECT `+profileCols+` FROM speaker_profiles ORDER BY display_name`);if e!=nil{return nil,e};defer rows.Close();var out []Profile;for rows.Next(){p,e:=scanProfile(rows);if e!=nil{return nil,e};out=append(out,p)};return out,rows.Err()}
func(s *PGStore)Profile(ctx context.Context,id string)(Profile,error){p,e:=scanProfile(s.db.QueryRow(ctx,`SELECT `+profileCols+` FROM speaker_profiles WHERE id=$1`,id));if e==pgx.ErrNoRows{return p,ErrNotFound};return p,e}
func(s *PGStore)AddConsent(ctx context.Context,c Consent)(Consent,error){if c.ID==""{c.ID=newID()};if c.At.IsZero(){c.At=time.Now().UTC()};_,e:=s.db.Exec(ctx,`INSERT INTO speaker_consents VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,c.ID,c.ProfileID,c.Permission,c.TextVersion,c.Granted,c.At,c.By,c.OrganiserHasWrittenPermission,c.RetentionDays);return c,e}
func(s *PGStore)Consents(ctx context.Context,id string)([]Consent,error){rows,e:=s.db.Query(ctx,`SELECT id,profile_id,permission,text_version,granted,at,by_role,organiser_written_permission,retention_days FROM speaker_consents WHERE profile_id=$1 ORDER BY at,id`,id);if e!=nil{return nil,e};defer rows.Close();var out []Consent;for rows.Next(){var c Consent;if e=rows.Scan(&c.ID,&c.ProfileID,&c.Permission,&c.TextVersion,&c.Granted,&c.At,&c.By,&c.OrganiserHasWrittenPermission,&c.RetentionDays);e!=nil{return nil,e};out=append(out,c)};return out,rows.Err()}
func(s *PGStore)AddSample(ctx context.Context,x Sample)(Sample,error){if x.ID==""{x.ID=newID()};b,_:=json.Marshal(x.Checks);_,e:=s.db.Exec(ctx,`INSERT INTO speaker_samples VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,x.ID,x.ProfileID,x.FileName,x.FilePath,x.Transcript,x.DurationSeconds,b,x.UploadedAt,x.DeleteAfter);return x,e}
func(s *PGStore)Samples(ctx context.Context,id string)([]Sample,error){rows,e:=s.db.Query(ctx,`SELECT id,profile_id,file_name,file_path,transcript,duration_seconds,checks,uploaded_at,delete_after FROM speaker_samples WHERE profile_id=$1 ORDER BY uploaded_at`,id);if e!=nil{return nil,e};defer rows.Close();var out []Sample;for rows.Next(){var x Sample;var b []byte;if e=rows.Scan(&x.ID,&x.ProfileID,&x.FileName,&x.FilePath,&x.Transcript,&x.DurationSeconds,&b,&x.UploadedAt,&x.DeleteAfter);e!=nil{return nil,e};json.Unmarshal(b,&x.Checks);out=append(out,x)};return out,rows.Err()}
func(s *PGStore)DeleteSample(ctx context.Context,id,sid string)(Sample,error){var x Sample;var b []byte;e:=s.db.QueryRow(ctx,`DELETE FROM speaker_samples WHERE profile_id=$1 AND id=$2 RETURNING id,profile_id,file_name,file_path,transcript,duration_seconds,checks,uploaded_at,delete_after`,id,sid).Scan(&x.ID,&x.ProfileID,&x.FileName,&x.FilePath,&x.Transcript,&x.DurationSeconds,&b,&x.UploadedAt,&x.DeleteAfter);if e==pgx.ErrNoRows{return x,ErrNotFound};json.Unmarshal(b,&x.Checks);return x,e}
func(s *PGStore)DeleteExpired(ctx context.Context,now time.Time)([]Sample,error){rows,e:=s.db.Query(ctx,`DELETE FROM speaker_samples WHERE delete_after<=$1 RETURNING id,profile_id,file_name,file_path,transcript,duration_seconds,checks,uploaded_at,delete_after`,now);if e!=nil{return nil,e};defer rows.Close();var out []Sample;for rows.Next(){var x Sample;var b []byte;if e=rows.Scan(&x.ID,&x.ProfileID,&x.FileName,&x.FilePath,&x.Transcript,&x.DurationSeconds,&b,&x.UploadedAt,&x.DeleteAfter);e!=nil{return nil,e};json.Unmarshal(b,&x.Checks);out=append(out,x)};return out,rows.Err()}
