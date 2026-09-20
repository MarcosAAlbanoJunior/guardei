CREATE TABLE items (
  id            INTEGER PRIMARY KEY,
  user_id       INTEGER NOT NULL,
  url           TEXT NOT NULL,
  canonical_url TEXT NOT NULL,
  platform      TEXT NOT NULL,
  title         TEXT,
  user_note     TEXT,
  transcript    TEXT,
  summary       TEXT,
  tags          TEXT,
  source        TEXT NOT NULL,
  embedding     BLOB,
  created_at    INTEGER NOT NULL,
  UNIQUE (user_id, canonical_url)
);

CREATE VIRTUAL TABLE items_fts USING fts5(
  title, user_note, summary, tags, transcript,
  content='items', content_rowid='id',
  tokenize='unicode61 remove_diacritics 2'
);

CREATE TRIGGER items_ai AFTER INSERT ON items BEGIN
  INSERT INTO items_fts(rowid, title, user_note, summary, tags, transcript)
  VALUES (new.id, new.title, new.user_note, new.summary, new.tags, new.transcript);
END;

CREATE TRIGGER items_ad AFTER DELETE ON items BEGIN
  INSERT INTO items_fts(items_fts, rowid, title, user_note, summary, tags, transcript)
  VALUES ('delete', old.id, old.title, old.user_note, old.summary, old.tags, old.transcript);
END;

CREATE TRIGGER items_au AFTER UPDATE OF title, user_note, summary, tags, transcript ON items BEGIN
  INSERT INTO items_fts(items_fts, rowid, title, user_note, summary, tags, transcript)
  VALUES ('delete', old.id, old.title, old.user_note, old.summary, old.tags, old.transcript);
  INSERT INTO items_fts(rowid, title, user_note, summary, tags, transcript)
  VALUES (new.id, new.title, new.user_note, new.summary, new.tags, new.transcript);
END;

CREATE TABLE pending (
  chat_id    INTEGER PRIMARY KEY,
  url        TEXT NOT NULL,
  reason     TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
