CREATE TABLE domains (
    id INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    domain VARCHAR (100) NOT NULL
);

CREATE TABLE paths (
    id        INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    domain_id INTEGER,
    path      VARCHAR (300),
    scheme    VARCHAR (5) NOT NULL,
    checked   INTEGER,
    FOREIGN KEY (
        domain_id
    )
    REFERENCES domains (id),
    UNIQUE KEY `unique_index` (`path`,`domain_id`),
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
);
