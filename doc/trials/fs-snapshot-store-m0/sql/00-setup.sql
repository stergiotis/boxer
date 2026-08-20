-- M0 setup: a scratch database and the facts-shaped scratch tables.
--
-- The column body is NOT repeated here: it is the `boxer.facts` TableDesc,
-- 185 physical columns, which the generated store emits. Take it from a
-- generated DDL file, e.g.
--
--   sed -n '/^\t"/p' public/keelson/runtime/sysmfacts/facts_ddl_clickhouse.out.sql
--
-- and splice it where <FACTS COLUMNS> appears below; `00-setup.sh` does that.
--
-- The database is `boxerm0`, not `boxer_m0`: recordstore/gen refuses a
-- Database that is not [a-z][a-z0-9]* (check 5, finding G2).
DROP DATABASE IF EXISTS boxerm0;
CREATE DATABASE boxerm0;
