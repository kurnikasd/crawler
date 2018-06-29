CREATE TABLE domains (
    id     INTEGER       PRIMARY KEY AUTOINCREMENT,
    domain VARCHAR (100) NOT NULL
);

CREATE TABLE params (
    param_name VARCHAR (20),
    path_id    INTEGER,
    param_type VARCHAR (5),
    value VARCHAR(1000),
    FOREIGN KEY (
        path_id
    )
    REFERENCES paths (id),
    PRIMARY KEY (
        param_name,
        path_id,
        param_type
    )
    ON CONFLICT IGNORE
);

CREATE TABLE paths (
    id        INTEGER       PRIMARY KEY AUTOINCREMENT,
    domain_id INTEGER,
    path      VARCHAR (300) UNIQUE ON CONFLICT IGNORE,
    scheme    VARCHAR (5) NOT NULL,
    checked   INTEGER,
    FOREIGN KEY (
        domain_id
    )
    REFERENCES domains (id)
);

CREATE TABLE alerts (
    time    DATETIME,
    module  VARCHAR (100),
    message VARCHAR (1000)
);
