\set app_pwd `echo "$GRAFANA_USER_PWD"` 

CREATE USER grafana WITH PASSWORD :'app_pwd';
