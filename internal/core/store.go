// Package core 实现本地优先的事件溯源核心:
// 原始输入不可覆盖,修订以新版本+父版本表达;派生值带算法版本、
// 参数指纹与直接来源;全部状态变化是机器可读取事件。
package core

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"
)

// Clock 提供逻辑时间戳;测试使用虚拟时间。
type Clock interface{ Now() int64 }

// ClockFunc 适配函数为 Clock。
type ClockFunc func() int64

// Now 实现 Clock。
func (f ClockFunc) Now() int64 { return f() }

const schema = `
CREATE TABLE IF NOT EXISTS meta(k TEXT PRIMARY KEY, v TEXT);
CREATE TABLE IF NOT EXISTS inputs(
  id TEXT PRIMARY KEY,
  parent TEXT,
  name TEXT,
  note TEXT,
  content BLOB,
  sha TEXT,
  seq INTEGER
);
CREATE TABLE IF NOT EXISTS events(
  seq INTEGER PRIMARY KEY,
  id TEXT UNIQUE,
  kind TEXT,
  actor TEXT,
  ts INTEGER,
  payload TEXT
);
CREATE TABLE IF NOT EXISTS parses(
  input_id TEXT,
  param_fp TEXT,
  seq INTEGER,
  payload TEXT,
  PRIMARY KEY(input_id, param_fp)
);
CREATE TABLE IF NOT EXISTS decisions(
  id TEXT PRIMARY KEY,
  seq INTEGER,
  target TEXT,
  action TEXT,
  actor TEXT,
  reason TEXT,
  undone_seq INTEGER
);`

// Store 是 SQLite 事件存储。
type Store struct {
	db    *sql.DB
	clock Clock
}

// Open 打开(或创建)存储并应用 schema。
func Open(path string, clock Clock) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	return &Store{db: db, clock: clock}, nil
}

// Close 关闭存储。
func (s *Store) Close() error { return s.db.Close() }

// DB 暴露底层连接(只读查询用)。
func (s *Store) DB() *sql.DB { return s.db }

// Event 是一条机器可读取事件。
type Event struct {
	Seq     int             `json:"seq"`
	ID      string          `json:"id"`
	Kind    string          `json:"kind"`
	Actor   string          `json:"actor"`
	TS      int64           `json:"ts"`
	Payload json.RawMessage `json:"payload"`
}

// Input 是一个不可覆盖的原始输入版本。
type Input struct {
	ID      string `json:"id"`
	Parent  string `json:"parent"`
	Name    string `json:"name"`
	Note    string `json:"note"`
	Content []byte `json:"-"`
	SHA     string `json:"sha"`
	Seq     int    `json:"seq"`
}

// Decision 是一次人工裁定(接受/否定/参数调整)。
type Decision struct {
	ID        string `json:"id"`
	Seq       int    `json:"seq"`
	Target    string `json:"target"`
	Action    string `json:"action"` // accept | reject
	Actor     string `json:"actor"`
	Reason    string `json:"reason"`
	UndoneSeq int    `json:"undoneSeq"` // 0 = 未撤销
}

func (s *Store) nextSeq() (int, error) {
	var seq int
	err := s.db.QueryRow(`SELECT COALESCE(MAX(seq),0)+1 FROM events`).Scan(&seq)
	return seq, err
}

// appendEvent 追加事件,seq 单调递增保证顺序稳定。
func (s *Store) appendEvent(kind, actor string, payload any) (Event, error) {
	seq, err := s.nextSeq()
	if err != nil {
		return Event{}, err
	}
	return s.appendEventAt(seq, kind, actor, s.clock.Now(), payload)
}

func (s *Store) appendEventAt(seq int, kind, actor string, ts int64, payload any) (Event, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return Event{}, err
	}
	id := eventID(seq, kind, b)
	_, err = s.db.Exec(`INSERT INTO events(seq,id,kind,actor,ts,payload) VALUES(?,?,?,?,?,?)`,
		seq, id, kind, actor, ts, string(b))
	if err != nil {
		return Event{}, fmt.Errorf("append event: %w", err)
	}
	return Event{Seq: seq, ID: id, Kind: kind, Actor: actor, TS: ts, Payload: b}, nil
}

func eventID(seq int, kind string, payload []byte) string {
	return fmt.Sprintf("evt:%04d:%s:%s", seq, kind, shortHash(payload))
}

// Events 返回 seq ≤ asOf 的全部事件(asOf=0 表示全部)。
func (s *Store) Events(asOf int) ([]Event, error) {
	q := `SELECT seq,id,kind,actor,ts,payload FROM events`
	args := []any{}
	if asOf > 0 {
		q += ` WHERE seq<=?`
		args = append(args, asOf)
	}
	q += ` ORDER BY seq`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var p string
		if err := rows.Scan(&e.Seq, &e.ID, &e.Kind, &e.Actor, &e.TS, &p); err != nil {
			return nil, err
		}
		e.Payload = json.RawMessage(p)
		out = append(out, e)
	}
	return out, rows.Err()
}

// MaxSeq 返回当前最大事件序号。
func (s *Store) MaxSeq() int {
	var n int
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(seq),0) FROM events`).Scan(&n)
	return n
}

func (s *Store) saveInput(in Input) error {
	_, err := s.db.Exec(`INSERT INTO inputs(id,parent,name,note,content,sha,seq) VALUES(?,?,?,?,?,?,?)`,
		in.ID, in.Parent, in.Name, in.Note, in.Content, in.SHA, in.Seq)
	return err
}

// Inputs 按提交顺序返回全部输入版本。
func (s *Store) Inputs() ([]Input, error) {
	rows, err := s.db.Query(`SELECT id,parent,name,note,content,sha,seq FROM inputs ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Input
	for rows.Next() {
		var in Input
		if err := rows.Scan(&in.ID, &in.Parent, &in.Name, &in.Note, &in.Content, &in.SHA, &in.Seq); err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// GetInput 读取一个输入版本。
func (s *Store) GetInput(id string) (*Input, error) {
	var in Input
	err := s.db.QueryRow(`SELECT id,parent,name,note,content,sha,seq FROM inputs WHERE id=?`, id).
		Scan(&in.ID, &in.Parent, &in.Name, &in.Note, &in.Content, &in.SHA, &in.Seq)
	if err != nil {
		return nil, err
	}
	return &in, nil
}

func (s *Store) saveParse(inputID, paramFP string, seq int, payload []byte) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO parses(input_id,param_fp,seq,payload) VALUES(?,?,?,?)`,
		inputID, paramFP, seq, string(payload))
	return err
}

// GetParse 读取指定输入与参数指纹下的解析结果。
func (s *Store) GetParse(inputID, paramFP string) (payload []byte, seq int, err error) {
	var p string
	err = s.db.QueryRow(`SELECT payload,seq FROM parses WHERE input_id=? AND param_fp=?`,
		inputID, paramFP).Scan(&p, &seq)
	if err != nil {
		return nil, 0, err
	}
	return []byte(p), seq, nil
}

// LatestParseBefore 返回 inputID 在 seq 之前最近的一次解析。
func (s *Store) LatestParseBefore(inputID string, asOf int) (payload []byte, seq int, err error) {
	var p string
	err = s.db.QueryRow(`SELECT payload,seq FROM parses WHERE input_id=? AND seq<=? ORDER BY seq DESC LIMIT 1`,
		inputID, asOf).Scan(&p, &seq)
	if err != nil {
		return nil, 0, err
	}
	return []byte(p), seq, nil
}

func (s *Store) saveDecision(d Decision) error {
	_, err := s.db.Exec(`INSERT INTO decisions(id,seq,target,action,actor,reason,undone_seq) VALUES(?,?,?,?,?,?,0)`,
		d.ID, d.Seq, d.Target, d.Action, d.Actor, d.Reason)
	return err
}

func (s *Store) markUndone(decisionID string, undoneSeq int) error {
	_, err := s.db.Exec(`UPDATE decisions SET undone_seq=? WHERE id=?`, undoneSeq, decisionID)
	return err
}

// Decisions 返回全部裁定记录。
func (s *Store) Decisions() ([]Decision, error) {
	rows, err := s.db.Query(`SELECT id,seq,target,action,actor,reason,undone_seq FROM decisions ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Decision
	for rows.Next() {
		var d Decision
		if err := rows.Scan(&d.ID, &d.Seq, &d.Target, &d.Action, &d.Actor, &d.Reason, &d.UndoneSeq); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
