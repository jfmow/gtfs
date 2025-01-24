package gtfs

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/jfmow/gtfs/realtime"
)

type Notification struct {
	Id                  int      `json:"id"`
	Endpoint            string   `json:"endpoint"`
	P256dh              string   `json:"p256dh"`
	Auth                string   `json:"auth"`
	Stop                string   `json:"stops"`
	RecentNotifications []string `json:"recent"`
	Created             int      `json:"created"`
}

func isValidURL(url string) bool {
	pattern := `^(https?|ftp):\/\/[^\s/$.?#].[^\s]*$`
	re := regexp.MustCompile(pattern)
	return re.MatchString(url)
}
func isBase64Url(s string) bool {
	// Checks if a string is a valid base64url encoded string
	// base64url is similar to base64, but uses URL-safe characters: "-" and "_"
	// Instead of "+" and "/"
	// Base64url strings may end with 0, 1, or 2 `=` characters
	_, err := base64.RawURLEncoding.DecodeString(s)
	return err == nil
}

var notificationMutex sync.Mutex

func (v Database) AddNotificationClient(endpoint, p256dh, auth, stopId string) error {
	_, err := v.GetStopByStopID(stopId)
	if err != nil {
		return errors.New("invalid stop id")
	}

	if len(p256dh) < 10 || !isBase64Url(p256dh) {
		return errors.New("invalid p256dh")
	}

	// Validate auth (at least 10 characters, base64url encoded)
	if len(auth) < 10 || !isBase64Url(auth) {
		return errors.New("invalid auth")
	}
	if !isValidURL(endpoint) {
		return errors.New("invalid endpoint")
	}

	query := `
		INSERT INTO notifications (endpoint, p256dh, auth, stop, created)
		VALUES (?, ?, ?, ?, ?);
	`
	_, err = v.db.Exec(query, endpoint, p256dh, auth, stopId, time.Now().In(v.timeZone).Unix())
	if err != nil {
		fmt.Println(err)
		return errors.New("failed to insert subscription")
	}
	return nil
}

func (v Database) removeNotificationClient(id int) {
	if id < 0 {
		return
	}
	query := `
		DELETE FROM notifications WHERE id =
	`

	v.db.Exec(query, id)
}

func (v Database) Notify(tripUpdates map[string]realtime.TripUpdate) error {
	if !notificationMutex.TryLock() {
		return errors.New("previous notifications haven't finished sending")
	}
	defer notificationMutex.Unlock()

	publicKey, found := os.LookupEnv("WP_PUB")
	if !found {
		return errors.New("missing pub key")
	}
	privateKey, found := os.LookupEnv("WP_PRIV")
	if !found {
		return errors.New("missing priv key")
	}

	if len(tripUpdates) == 0 {
		return errors.New("no trip updates")
	}

	var canceledTrips []string //Id's of the trips

	for _, update := range tripUpdates {
		tripStatus := update.Trip.ScheduleRelationship
		tripId := update.Trip.TripID
		if tripStatus == 3 {
			canceledTrips = append(canceledTrips, tripId)
		}
	}

	//Check if there is any canceled trips
	if len(canceledTrips) == 0 {
		return errors.New("no canceled trips found")
	}

	//There is in fact canceled trips

	clients, err := getNotificationClients(v)

	if err != nil {
		return err
	}

	for _, client := range clients {
		var canceledServices []string
		var canceledTripIds []string
		var stopName string
		for _, stop := range client.Stops {

			//Get the current time
			now := time.Now().In(v.timeZone)
			currentWeekDay := now.Weekday().String()
			currentTime := now.Format("15:04:05")
			dateString := now.Format("20060102")

			//Get the services stopping at the clients stop
			services, err := v.GetActiveTrips(dateString, currentWeekDay, stop, currentTime, 15)
			if err != nil {
				fmt.Printf("No services found for stop: %s\n", stop)
				continue
			}

			for _, service := range services {
				if stopName == "" {
					stopName = service.StopData.StopName
				}
				if contains(client.RecentNotifications, service.TripID) {
					//Notification already sent
					continue
				}
				tripId := service.TripID

				//Check if the trip is canceled
				if contains(canceledTrips, tripId) {
					//Trip has been canceled at given stop
					//Add to notification

					parsedTime, err := time.Parse("15:04:05", service.ArrivalTime)
					if err != nil {
						fmt.Println("Error parsing time:", err)
						continue
					}

					// Format the time in 12-hour format with AM/PM
					formattedTime := parsedTime.Format("3:04pm")
					canceledServices = append(canceledServices, fmt.Sprintf("%s to %s | (%s)", formattedTime, service.StopHeadsign, service.TripData.RouteID))
					canceledTripIds = append(canceledTripIds, service.TripID)
				}
			}

		}

		if len(canceledServices) == 0 {
			continue //skip (no canceled services)
		}

		//There are canceled services
		//Notify the user
		go func(client NotificationClient) {
			payload := map[string]string{
				"title": fmt.Sprintf("NEW CANCELLATIONS at %s", stopName),
				"body":  strings.Join(canceledServices, "\n"),
			}
			payloadBytes, _ := json.Marshal(payload)

			// Send Notification
			resp, err := webpush.SendNotification(payloadBytes, &client.Notification, &webpush.Options{
				Subscriber:      v.mailToEmail,
				VAPIDPublicKey:  publicKey,
				VAPIDPrivateKey: privateKey,
				TTL:             30,
			})
			if err != nil || resp.StatusCode == 410 {
				v.removeNotificationClient(client.Id)
			} else {
				updateNotificationTripsByID(v, client.Id, append(canceledTripIds, client.RecentNotifications...))
			}
			defer resp.Body.Close()
		}(client)
	}

	return nil
}

type NotificationClient struct {
	Id                  int
	Notification        webpush.Subscription
	Stops               []string
	RecentNotifications []string
}

func getNotificationClients(v Database) ([]NotificationClient, error) {
	query := `
		SELECT id, endpoint, p256dh, auth, stop, created, recent_notifications
		FROM notifications
	`

	rows, err := v.db.Query(query)
	if err != nil {
		fmt.Println("Error querying database:", err)
		return nil, errors.New("problem reading rows")
	}
	defer rows.Close()

	var notificationsToSend []NotificationClient

	// Process each row
	for rows.Next() {
		var notification Notification
		var recent string

		// Scan the database row
		err := rows.Scan(
			&notification.Id,
			&notification.Endpoint,
			&notification.P256dh,
			&notification.Auth,
			&notification.Stop,
			&notification.Created,
			&recent,
		)
		if err != nil {
			fmt.Println("Error scanning row:", err)
			continue
		}

		if recent != "" {
			err = json.Unmarshal([]byte(recent), &notification.RecentNotifications)
			if err != nil {
				//Invalid recents
				v.removeNotificationClient(notification.Id)
				continue
			}
		}

		stops, err := v.GetChildStopsByParentStopID(notification.Stop)
		if err != nil || len(stops) == 0 {
			//Invalid stop
			v.removeNotificationClient(notification.Id)
			continue
		}

		var stopIds []string

		for _, stop := range stops {
			stopIds = append(stopIds, stop.StopId)
		}

		if time.Now().In(v.timeZone).After(time.Unix(int64(notification.Created), 0).Add(30 * 24 * time.Hour)) {
			// Action if 30 days have passed
			v.removeNotificationClient(notification.Id)
			continue
		}

		notificationsToSend = append(notificationsToSend, NotificationClient{
			Id: notification.Id,
			Notification: webpush.Subscription{
				Endpoint: notification.Endpoint,
				Keys: webpush.Keys{
					Auth:   notification.Auth,
					P256dh: notification.P256dh,
				},
			},
			Stops:               stopIds,
			RecentNotifications: notification.RecentNotifications,
		})
	}

	if len(notificationsToSend) == 0 {
		return nil, errors.New("no notification clients found")
	}

	return notificationsToSend, nil
}

func updateNotificationTripsByID(v Database, id int, updatedTripIds []string) error {
	// Construct the UPDATE SQL query
	query := `
        UPDATE notifications
        SET 
            recent_notifications = ?
        WHERE id = ?;
    `

	// Marshal recent notifications to JSON if necessary
	recentNotificationsJSON, err := json.Marshal(updatedTripIds)
	if err != nil {
		return fmt.Errorf("error marshalling recent notifications: %v", err)
	}

	// Execute the query with the updated values
	_, err = v.db.Exec(query,
		recentNotificationsJSON,
		id,
	)

	if err != nil {
		fmt.Println("Error updating notification:", err)
		return fmt.Errorf("failed to update notification: %v", err)
	}

	return nil
}

func (v Database) FindNotificationClient(endpoint, p256dh, auth, stopId string) (*Notification, error) {
	if len(p256dh) < 10 || !isBase64Url(p256dh) {
		return nil, errors.New("invalid p256dh")
	}

	// Validate auth (at least 10 characters, base64url encoded)
	if len(auth) < 10 || !isBase64Url(auth) {
		return nil, errors.New("invalid auth")
	}
	if !isValidURL(endpoint) {
		return nil, errors.New("invalid endpoint")
	}

	query := `
		SELECT id, endpoint, p256dh, auth, stop, created, recent_notifications
		FROM notifications
		WHERE endpoint = ? AND p256dh = ? AND auth = ?
	`

	if stopId != "" {
		query += `AND stops = ?`
	}

	row := v.db.QueryRow(query, endpoint, p256dh, auth, stopId)

	// Process each row
	var notification Notification
	var recent string

	// Scan the database row
	err := row.Scan(
		&notification.Id,
		&notification.Endpoint,
		&notification.P256dh,
		&notification.Auth,
		&notification.Stop,
		&notification.Created,
		&recent,
	)
	if err != nil {
		//fmt.Println("Error scanning row:", err)
		return nil, errors.New("problem scanning row")
	}

	if recent != "" {
		err = json.Unmarshal([]byte(recent), &notification.RecentNotifications)
		if err != nil {
			//Invalid recents
			v.removeNotificationClient(notification.Id)
			return nil, errors.New("invalid recent's (!!row removed!!)")
		}
	}

	return &notification, nil
}

func (v Database) RemoveNotificationClient(endpoint, p256dh, auth, stopId string) error {
	if len(p256dh) < 10 || !isBase64Url(p256dh) {
		return errors.New("invalid p256dh")
	}

	// Validate auth (at least 10 characters, base64url encoded)
	if len(auth) < 10 || !isBase64Url(auth) {
		return errors.New("invalid auth")
	}
	if !isValidURL(endpoint) {
		return errors.New("invalid endpoint")
	}

	query := `
		DELETE FROM notifications WHERE endpoint = ? AND p256dh = ? AND auth = ?
	`

	if stopId != "" {
		query += "AND stop = ?"
	}

	_, err := v.db.Exec(query, endpoint, p256dh, auth, stopId)
	if err != nil {
		return errors.New("failed to delete subscription")
	}
	return nil
}
