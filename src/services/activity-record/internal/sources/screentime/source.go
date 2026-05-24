//go:build darwin

package screentime

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"arkloop/services/activity-record/internal/store"
	_ "modernc.org/sqlite"
)

type Source struct {
	dbPath string
}

type cursor struct {
	MaxStartDate float64 `json:"max_start_date"`
}

func NewDefault() (*Source, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(home, "Library", "Application Support", "Knowledge", "knowledgeC.db")
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("knowledgeC.db not accessible (Full Disk Access required): %w", err)
	}
	return &Source{dbPath: dbPath}, nil
}

func (s *Source) Name() string {
	return "screentime"
}

var trackedStreams = []string{
	"/app/usage",
	"/display/isBacklit",
	"/notification/usage",
	"/app/mediaUsage",
	"/media/nowPlaying",
	"/app/webUsage",
	"/bluetooth/isConnected",
}

func (s *Source) Sync(ctx context.Context, db *store.Store) (int, error) {
	var cur cursor
	if err := db.Cursor(ctx, s.Name(), &cur); err != nil {
		return 0, err
	}
	tmp, err := copyDB(s.dbPath)
	if err != nil {
		return 0, fmt.Errorf("copy knowledgeC.db: %w", err)
	}
	defer os.Remove(tmp)

	srcDB, err := sql.Open("sqlite", "file:"+tmp+"?mode=ro&immutable=1")
	if err != nil {
		return 0, err
	}
	defer srcDB.Close()

	rows, err := srcDB.QueryContext(ctx, `
SELECT
  o.Z_PK,
  o.ZSTREAMNAME,
  o.ZSTARTDATE,
  o.ZENDDATE,
  COALESCE(o.ZVALUESTRING, ''),
  COALESCE(o.ZVALUEINTEGER, 0),
  COALESCE(src.ZBUNDLEID, ''),
  COALESCE(src.ZDEVICEID, ''),
  COALESCE(sm.Z_DKNOTIFICATIONUSAGEMETADATAKEY__BUNDLEID, ''),
  COALESCE(sm.Z_DKNOWPLAYINGMETADATAKEY__TITLE, ''),
  COALESCE(sm.Z_DKNOWPLAYINGMETADATAKEY__ARTIST, ''),
  COALESCE(sm.Z_DKNOWPLAYINGMETADATAKEY__ALBUM, ''),
  COALESCE(sm.Z_DKNOWPLAYINGMETADATAKEY__GENRE, ''),
  COALESCE(sm.Z_DKNOWPLAYINGMETADATAKEY__DURATION, 0),
  COALESCE(sm.Z_DKNOWPLAYINGMETADATAKEY__PLAYING, 0),
  COALESCE(sm.Z_DKDIGITALHEALTHMETADATAKEY__WEBDOMAIN, ''),
  COALESCE(sm.Z_DKDIGITALHEALTHMETADATAKEY__WEBPAGEURL, ''),
  COALESCE(sm.Z_DKAPPMEDIAUSAGEMETADATAKEY__URL, ''),
  COALESCE(sm.Z_DKAPPMEDIAUSAGEMETADATAKEY__MEDIAURL, ''),
  COALESCE(sm.Z_DKBLUETOOTHMETADATAKEY__NAME, ''),
  COALESCE(sm.Z_DKBLUETOOTHMETADATAKEY__DEVICETYPE, 0),
  COALESCE(sm.Z_DKBLUETOOTHMETADATAKEY__ISAPPLEAUDIODEVICE, 0)
FROM ZOBJECT o
LEFT JOIN ZSOURCE src ON src.Z_PK = o.ZSOURCE
LEFT JOIN ZSTRUCTUREDMETADATA sm ON sm.Z_PK = o.ZSTRUCTUREDMETADATA
WHERE o.ZSTREAMNAME IN ('/app/usage', '/display/isBacklit', '/notification/usage', '/app/mediaUsage', '/media/nowPlaying', '/app/webUsage', '/bluetooth/isConnected')
  AND o.ZSTARTDATE > ?
ORDER BY o.ZSTARTDATE ASC
`, cur.MaxStartDate)
	if err != nil {
		return 0, fmt.Errorf("query knowledgeC.db: %w", err)
	}

	var events []store.Event
	next := cur
	for rows.Next() {
		var (
			pk             int64
			stream         string
			startDate      float64
			endDate        float64
			valueString    string
			valueInteger   int64
			sourceBundleID string
			deviceID       string
			notifBundleID  string
			npTitle        string
			npArtist       string
			npAlbum        string
			npGenre        string
			npDuration     float64
			npPlaying      int64
			webDomain      string
			webURL         string
			mediaURL       string
			mediaMediaURL  string
			btName         string
			btDeviceType   int64
			btIsAppleAudio int64
		)
		if err := rows.Scan(
			&pk, &stream, &startDate, &endDate,
			&valueString, &valueInteger,
			&sourceBundleID, &deviceID,
			&notifBundleID,
			&npTitle, &npArtist, &npAlbum, &npGenre, &npDuration, &npPlaying,
			&webDomain, &webURL,
			&mediaURL, &mediaMediaURL,
			&btName, &btDeviceType, &btIsAppleAudio,
		); err != nil {
			rows.Close()
			return 0, err
		}
		if startDate > next.MaxStartDate {
			next.MaxStartDate = startDate
		}

		ev, ok := buildEvent(pk, stream, startDate, endDate, valueString, valueInteger,
			sourceBundleID, deviceID, notifBundleID,
			npTitle, npArtist, npAlbum, npGenre, npDuration, npPlaying,
			webDomain, webURL, mediaURL, mediaMediaURL,
			btName, btDeviceType, btIsAppleAudio)
		if !ok {
			continue
		}
		events = append(events, ev)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	inserted, err := db.UpsertEvents(ctx, events)
	if err != nil {
		_ = db.SaveCursor(ctx, s.Name(), cur, err)
		return 0, err
	}
	if err := db.SaveCursor(ctx, s.Name(), next, nil); err != nil {
		return 0, err
	}
	return inserted, nil
}

func buildEvent(
	pk int64, stream string,
	startDate, endDate float64,
	valueString string, valueInteger int64,
	sourceBundleID, deviceID string,
	notifBundleID string,
	npTitle, npArtist, npAlbum, npGenre string, npDuration float64, npPlaying int64,
	webDomain, webURL string,
	mediaURL, mediaMediaURL string,
	btName string, btDeviceType, btIsAppleAudio int64,
) (store.Event, bool) {
	base := store.Event{
		Source:     "screentime",
		OccurredAt: cocoaTime(startDate),
	}
	duration := endDate - startDate
	if duration < 0 {
		duration = 0
	}

	switch stream {
	case "/app/usage":
		if valueString == "" {
			return base, false
		}
		appName := readableAppName(valueString)
		base.SourceEventID = fmt.Sprintf("screentime:%d", pk)
		base.App = appName
		base.Action = "app_used"
		base.Title = appName
		base.Metadata = map[string]any{
			"bundle_id":     valueString,
			"source_bundle": sourceBundleID,
			"device_id":     deviceID,
			"duration_sec":  duration,
		}
		base.RefKind = "bundle_id"
		base.RefKey = valueString

	case "/display/isBacklit":
		base.SourceEventID = fmt.Sprintf("screentime:backlit:%d", pk)
		if valueInteger == 1 {
			base.Action = "screen_on"
			base.Title = "screen on"
		} else {
			base.Action = "screen_off"
			base.Title = "screen off"
		}
		base.Metadata = map[string]any{
			"duration_sec": duration,
		}

	case "/notification/usage":
		bundleID := notifBundleID
		if bundleID == "" {
			bundleID = sourceBundleID
		}
		if bundleID == "" {
			return base, false
		}
		base.SourceEventID = fmt.Sprintf("screentime:notif:%d", pk)
		base.App = readableAppName(bundleID)
		base.Action = "notification_received"
		base.Title = readableAppName(bundleID)
		base.Metadata = map[string]any{
			"bundle_id": bundleID,
		}
		base.RefKind = "bundle_id"
		base.RefKey = bundleID

	case "/app/mediaUsage":
		if valueString == "" {
			return base, false
		}
		appName := readableAppName(valueString)
		base.SourceEventID = fmt.Sprintf("screentime:media:%d", pk)
		base.App = appName
		base.Action = "media_used"
		base.Title = appName
		meta := map[string]any{
			"bundle_id":    valueString,
			"duration_sec": duration,
		}
		if mediaURL != "" {
			meta["url"] = mediaURL
		}
		if mediaMediaURL != "" {
			meta["media_url"] = mediaMediaURL
		}
		base.Metadata = meta

	case "/media/nowPlaying":
		if valueString == "" {
			return base, false
		}
		appName := readableAppName(valueString)
		base.SourceEventID = fmt.Sprintf("screentime:np:%d", pk)
		base.App = appName
		base.Action = "media_playing"
		if npTitle != "" {
			base.Title = npTitle
		} else {
			base.Title = appName
		}
		meta := map[string]any{
			"bundle_id":    valueString,
			"duration_sec": npDuration,
			"playing":      npPlaying == 1,
		}
		if npArtist != "" {
			meta["artist"] = npArtist
		}
		if npAlbum != "" {
			meta["album"] = npAlbum
		}
		if npGenre != "" {
			meta["genre"] = npGenre
		}
		base.Metadata = meta

	case "/app/webUsage":
		if valueString == "" {
			return base, false
		}
		appName := readableAppName(valueString)
		base.SourceEventID = fmt.Sprintf("screentime:web:%d", pk)
		base.App = appName
		base.Action = "web_used"
		base.Title = webDomain
		if base.Title == "" {
			base.Title = appName
		}
		base.URL = webURL
		meta := map[string]any{
			"bundle_id":  valueString,
			"web_domain": webDomain,
		}
		if duration > 0 {
			meta["duration_sec"] = duration
		}
		base.Metadata = meta
		if webURL != "" {
			base.RefKind = "url"
			base.RefKey = webURL
		}

	case "/bluetooth/isConnected":
		base.SourceEventID = fmt.Sprintf("screentime:bt:%d", pk)
		if valueInteger == 1 {
			base.Action = "bluetooth_connected"
			base.Title = btName
		} else {
			base.Action = "bluetooth_disconnected"
			base.Title = btName
		}
		if base.Title == "" {
			base.Title = "bluetooth"
		}
		base.Metadata = map[string]any{
			"device_name":          btName,
			"device_type":          btDeviceType,
			"is_apple_audio_device": btIsAppleAudio == 1,
			"duration_sec":         duration,
		}

	default:
		return base, false
	}
	return base, true
}

var cocoaEpoch = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

func cocoaTime(seconds float64) time.Time {
	return cocoaEpoch.Add(time.Duration(seconds * float64(time.Second))).UTC()
}

var genericLastParts = map[string]bool{
	"app":      true,
	"desktop":  true,
	"electron": true,
	"helper":   true,
	"client":   true,
	"main":     true,
	"agent":    true,
}

func readableAppName(bundleID string) string {
	parts := strings.Split(bundleID, ".")
	if len(parts) <= 1 {
		return bundleID
	}
	last := parts[len(parts)-1]
	if isGenericPart(last) && len(parts) >= 2 {
		return parts[len(parts)-2]
	}
	return last
}

func isGenericPart(s string) bool {
	if genericLastParts[strings.ToLower(s)] {
		return true
	}
	if len(s) <= 1 {
		return true
	}
	if len(s) >= 8 {
		digits := 0
		for _, c := range s {
			if c >= '0' && c <= '9' {
				digits++
			}
		}
		if float64(digits)/float64(len(s)) > 0.3 {
			return true
		}
	}
	return false
}

func copyDB(path string) (string, error) {
	input, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer input.Close()
	tmp, err := os.CreateTemp("", "arkloop-screentime-*.sqlite")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	if _, err := io.Copy(tmp, input); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	return tmpPath, nil
}
