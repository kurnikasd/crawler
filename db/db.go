package db

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}

type DbInstance struct {
	DBPath string
	dbi    *sql.DB
}

func (x *DbInstance) GetDbInstance() {
	dbi, err := sql.Open("sqlite3", x.DBPath)
	checkErr(err)
	x.dbi = dbi
}

func (x *DbInstance) CloseDB() {
	x.dbi.Close()
}

func (x *DbInstance) GetDomains() []string {
	rows, err := x.dbi.Query("SELECT domain from domains")
	checkErr(err)
	var domain string
	domain_list := make([]string, 0)
	for rows.Next() {
		err = rows.Scan(&domain)
		checkErr(err)
		domain_list = append(domain_list, domain)
	}
	rows.Close()
	return domain_list
}

func (x *DbInstance) InsertURL(url string, domain string) {
	var domain_id string
	stmt, err := x.dbi.Prepare("SELECT id FROM domains WHERE domain = ?")
	checkErr(err)
	err = stmt.QueryRow(domain).Scan(&domain_id)
	checkErr(err)
	_, err = x.dbi.Exec("INSERT INTO paths VALUES (?,?)", domain_id, domain)
	checkErr(err)
}

func (x *DbInstance) GetDomainId(domain string) int {
	var domain_id int
	stmt, err := x.dbi.Prepare("SELECT id FROM domains WHERE domain = ?")
	checkErr(err)
	err = stmt.QueryRow(domain).Scan(&domain_id)
	return domain_id
}

func (x *DbInstance) GetPathId(domain_id int, path string) int {
	var path_id int
	stmt, err := x.dbi.Prepare("SELECT id FROM paths WHERE domain_id = ? AND path = ?")
	checkErr(err)
	err = stmt.QueryRow(domain_id, path).Scan(&path_id)
	return path_id
}

func (x *DbInstance) GetParams(domain_id int, path_id int) []string {
	stmt, err := x.dbi.Prepare("SELECT param_name " +
		"FROM params JOIN paths ON params.path_id = paths.id " +
		"WHERE domain_id = ? AND path_id = ?")
	checkErr(err)
	rows, err := stmt.Query(domain_id, path_id)

	if err != nil {
		panic(err)
	}
	defer rows.Close()

	var result []string

	for rows.Next() {
		var param_name string
		rows.Scan(&param_name)
		result = append(result, param_name)
	}

	return result
}

func (x *DbInstance) AddPathByDomainId(path string, domain_id int) {
	_, err := x.dbi.Exec("INSERT INTO paths (domain_id, path) VALUES (?,?)", domain_id, path)
	checkErr(err)
}

func (x *DbInstance) AddParamByPathId(param string, param_type string, path_id int) {
	_, err := x.dbi.Exec("INSERT INTO params (path_id, param_name, param_type) VALUES (?,?,?)", path_id, param, param_type)
	checkErr(err)
}
