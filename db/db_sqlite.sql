CREATE TABLE domains (
    id     INTEGER       PRIMARY KEY AUTOINCREMENT,
    domain VARCHAR (100) NOT NULL
);

CREATE TABLE params (
    param_name VARCHAR (20),
    path_id    INTEGER,
    param_type VARCHAR (5),
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
    path      VARCHAR (100) UNIQUE ON CONFLICT IGNORE,
    FOREIGN KEY (
        domain_id
    )
    REFERENCES domains (id) 
);
