CREATE TABLE reset_state (
    id INTEGER PRIMARY KEY CHECK(id=1),
    epoch INTEGER NOT NULL DEFAULT 0,
    factory_epoch INTEGER NOT NULL DEFAULT 0,
    factory_pending INTEGER NOT NULL DEFAULT 0
);
INSERT INTO reset_state(id) VALUES(1);
