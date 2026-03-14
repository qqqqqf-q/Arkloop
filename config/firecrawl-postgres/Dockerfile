# Firecrawl NUQ PostgreSQL with pg_cron
# Schema sourced from: https://github.com/firecrawl/firecrawl/blob/main/apps/nuq-postgres/
# Update nuq.sql when upgrading firecrawl image version

ARG PG_MAJOR=17
FROM postgres:${PG_MAJOR}

RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        postgresql-${PG_MAJOR}-cron; \
    rm -rf /var/lib/apt/lists/*

RUN set -eux; \
    conf_sample="/usr/share/postgresql/${PG_MAJOR}/postgresql.conf.sample"; \
    sed -ri "s/^#?shared_preload_libraries\s*=.*/shared_preload_libraries = 'pg_cron'/" "$conf_sample"; \
    printf "\n# pg_cron\ncron.database_name = 'firecrawl'\n" >> "$conf_sample"

COPY nuq.sql /docker-entrypoint-initdb.d/010-nuq.sql
