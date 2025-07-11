# GO GTFS

This Go package provides utilities to process [GTFS (General Transit Feed Specification)](https://gtfs.org/) data and GTFS-realtime feeds. It is designed for easy integration into Go projects to ingest, transform, and use both static and real-time transit data.

## Features

- **Static GTFS Processor:**  
  - Load and parse static GTFS datasets (e.g., `stops.txt`, `routes.txt`, `trips.txt`).
  - Query, transform, and utilize transit schedule and network information.

- **GTFS-realtime Processor:**  
  - Connect to and parse GTFS-realtime feeds: vehicle positions, trip updates, and service alerts.
  - Useful for real-time transit applications and analytics.

## Usage

Add this package to your Go module:

```go
import (
    "github.com/jfmow/gtfs"
    rt "github.com/jfmow/gtfs/realtime"
    "fmt"
    "time"
)

// Example: Load static GTFS data
AucklandTransportGTFSData, err := gtfs.New(
    "https://gtfs.at.govt.nz/gtfs.zip", // GTFS static feed URL
    gtfs.ApiKey{Header: "", Value: ""}, // Optional API key for downloads
    "atfgtfs",                          // Database name or identifier
    localTimeZone,                      // Your local time zone
    "hi@example.com",                    // Contact email for GTFS feed usage
)
if err != nil {
    fmt.Println("Error loading GTFS db:", err)
}

// Example: Connect to GTFS-realtime feeds
AucklandTransportRealtimeData, err := rt.NewClient(
    atApiKey,                                       // Your API key value
    "Ocp-Apim-Subscription-Key",                    // API key header name
    10*time.Second,                                 // Realtime data refresh period
    "https://api.at.govt.nz/realtime/legacy/vehiclelocations", // Vehicle positions feed URL
    "https://api.at.govt.nz/realtime/legacy/tripupdates",      // Trip updates feed URL
    "https://api.at.govt.nz/realtime/legacy/servicealerts",    // Service alerts feed URL
)
if err != nil {
    panic(err)
}
```

## Setup Variables

If you want to manage configuration via environment variables, use the following (these are typical, check your codebase for precise usage):

| Variable Name              | Description                                  | Example Value                               |
|---------------------------|----------------------------------------------|---------------------------------------------|
| `GTFS_STATIC_URL`         | URL for static GTFS dataset                  | `https://agency.org/gtfs.zip`               |
| `GTFS_REALTIME_VEHICLE_URL` | Realtime vehicle positions feed URL (PROTOBUF)         | `https://agency.org/vehiclelocations.pb`    |
| `GTFS_REALTIME_TRIP_URL`    | Realtime trip updates feed URL (PROTOBUF)              | `https://agency.org/tripupdates.pb`         |
| `GTFS_REALTIME_ALERT_URL`   | Realtime service alerts feed URL (PROTOBUF)            | `https://agency.org/servicealerts.pb`       |
| `GTFS_API_KEY`            | API auth value (if required)                  | `yourapikey`                                |
| `GTFS_API_KEY_HEADER`     | API auth header name for http req                          | `Ocp-Apim-Subscription-Key`                 |
| `GTFS_DB_NAME`            | Database name or identifier                  | `atfgtfs`                                   |
| `GTFS_TIMEZONE`           | Local time zone                              | `Pacific/Auckland`                          |
| `GTFS_CONTACT_EMAIL`      | Contact email for usage                      | `hi@example.com`                             |

## Setup

1. **Install the package:**

    ```bash
    go get github.com/jfmow/gtfs
    ```

2. **Use in your Go program as shown above.**

## Contributing

Contributions, bug reports, and feature requests are welcome! Please open an issue or submit a pull request.

## License

See [LICENSE](LICENSE) for details.

---

For questions or support, contact [@jfmow](https://github.com/jfmow).

---

### Generating gtfs caches

When using the gtfs data it can be really in effecent to hit the database everytime you want to find a stop or route or combonation. So instead you can use this cache function to request the data at startup from the database, which then stores it in memory and on the refresh period defined will refresh the data. You can then call the function returned to get the cached data.

```go
getRouteCache, err := gtfs.GenerateACache(gtfsData.GetRoutes, func(routes []gtfs.Route) (map[string]gtfs.Route, error) {
  newCache := make(map[string]gtfs.Route)
  for _, route := range routes {
    newCache[route.RouteId] = route
  }
  return newCache, nil
}, make(map[string]gtfs.Route, 0), gtfsData)
if err != nil {
  log.Printf("Failed to init routes cache: %v", err)
}
```
Using the cached data in a http route:
```go
routesRoute.GET("/:routeId", func(c echo.Context) error {
  routeIdEncoded := c.PathParam("routeId")
  routeId, err := url.PathUnescape(routeIdEncoded)
  if err != nil {
    return JsonApiResponse(c, http.StatusBadRequest, "invalid route id", nil, ResponseDetails("routeId", routeIdEncoded, "details", "Invalid route ID format", "error", err.Error()))
  }

  //Get the data from memory 
  cachedRoutes := getRouteCache()
  //Find the route
  route, ok := cachedRoutes[routeId]

  if !ok {
    return JsonApiResponse(c, http.StatusNotFound, "", nil, ResponseDetails("routeId", routeId, "details", "No route found for the given route ID in the cache"))
  }

  return JsonApiResponse(c, http.StatusOK, "", route)
})
```
