# Versioned migrations, Atlas style (same workflow as the finance project).
#
# internal/store/schema/ is the desired state. To change the schema:
#   1. edit the files in internal/store/schema/
#   2. atlas migrate diff <name> --env local   (writes YYYYMMDDHHmmSS_<name>.sql + atlas.sum)
#   3. atlas migrate lint --env local --latest 1
#   4. make generate                            (sqlc reads internal/store/schema)
#
# The binary applies internal/store/migrations itself on startup (embedded,
# checksum-verified against atlas.sum), so `atlas migrate apply` is not needed.
env "local" {
  src = "file://internal/store/schema"
  dev = "sqlite://dev?mode=memory"

  migration {
    dir = "file://internal/store/migrations"
  }
}
