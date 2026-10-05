# Migrations image: golang-migrate with this repo's migrations baked in, so a
# deployment doesn't need the source tree. Build from the repo root:
#
#   docker build -f deploy/migrate/Dockerfile -t tft-platform-migrate .
FROM migrate/migrate:v4.17.1
COPY internal/db/migrations /migrations
ENTRYPOINT ["migrate", "-path", "/migrations"]
CMD ["up"]
