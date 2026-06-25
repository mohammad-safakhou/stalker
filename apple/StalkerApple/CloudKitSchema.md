# CloudKit Schema

Container:

```text
iCloud.com.mohammad-safakhou.stalker
```

Database: private.

## Device

Record name: `device.id`

- `name`: String
- `platform`: String
- `lastSeen`: Date

## LiveSnapshot

Record name: `device.id`

- `deviceID`: String
- `inputTokens`: Int64
- `outputTokens`: Int64
- `liveInputTokens`: Int64
- `liveOutputTokens`: Int64
- `requests`: Int64
- `errors`: Int64
- `cursor`: String
- `updatedAt`: Date

## StatsBucketHourly

Record name: `deviceID:hourly:<RFC3339 hour>`

- `deviceID`: String
- `start`: Date
- `inputTokens`: Int64
- `outputTokens`: Int64
- `requests`: Int64
- `errors`: Int64
- `streams`: Int64

## StatsBucketDaily

Record name: `deviceID:daily:<RFC3339 day>`

Same fields as `StatsBucketHourly`.

## TokenTotal

Record name: `<side>:<tokenHash>`

- `side`: String
- `token`: String
- `tokenHash`: String
- `occurrences`: Int64

No prompt text, response text, request body, response body, previews, body paths,
authorization headers, cookies, or raw exchange payloads are synced.
