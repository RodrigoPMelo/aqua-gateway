package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"firebase.google.com/go"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/joho/godotenv"
	"google.golang.org/api/option"
)

type SensorData struct {
	DeviceID  string    `json:"device_id"`
	Data      string    `json:"data"`
	Timestamp time.Time `json:"timestamp"`
}

var (
	client     *firestore.Client
	dataBuffer = make(map[string][]SensorData)
	dataMutex  sync.Mutex
)

func initFirebase() {
	ctx := context.Background()
	sa := option.WithCredentialsFile("firebase_service_account.json")
	app, err := firebase.NewApp(ctx, nil, sa)
	if err != nil {
		log.Printf("Error initializing Firebase app: %v", err)
		return
	}
	client, err = app.Firestore(ctx)
	if err != nil {
		log.Printf("Error initializing Firestore client: %v", err)
		return
	}
	log.Println("Firebase initialized successfully.")
}

func addToBuffer(data SensorData) {
	dataMutex.Lock()
	defer dataMutex.Unlock()

	deviceID := data.DeviceID
	dataBuffer[deviceID] = append(dataBuffer[deviceID], data)
	log.Printf("Added data to buffer for device %s: %v", deviceID, data)
}

func saveToFile() {
	dataPath := os.Getenv("DATA_PATH")
	dataMutex.Lock()
	defer dataMutex.Unlock()

	fileName := fmt.Sprintf(dataPath + "/sensor_data_%s.json", time.Now().Format("2006-01-02"))
	fileData, err := json.Marshal(dataBuffer)
	if err != nil {
		log.Printf("Error marshaling buffer data to JSON: %v", err)
		return
	}
	err = os.WriteFile(fileName, fileData, 0644)
	if err != nil {
		log.Printf("Error saving buffer to file %s: %v", fileName, err)
		return
	}
	log.Printf("Saved buffer data to file: %s", fileName)
}

func uploadToFirebase() {
	dataPath := os.Getenv("DATA_PATH")
	entries, err := os.ReadDir(dataPath + "/")
	if err != nil {
		log.Printf("Error reading directory /data/: %v", err)
		return
	}

	infos := make([]fs.FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			log.Printf("Error getting file info: %v", err)
			continue
		}
		infos = append(infos, info)
	}

	ctx := context.Background()
	batch := client.BulkWriter(ctx)

	for _, file := range infos {
		if !file.IsDir() {
			filePath := dataPath + "/" + file.Name()

			data, err := os.ReadFile(filePath)
			if err != nil {
				log.Printf("Error reading file %s: %v", file.Name(), err)
				continue
			}

			var sensorData map[string][]SensorData
			if err := json.Unmarshal(data, &sensorData); err != nil {
				log.Printf("Error unmarshaling JSON for file %s: %v", file.Name(), err)
				continue
			}

			for deviceID, records := range sensorData {
				for _, record := range records {
					docID := fmt.Sprintf("%s_%s", deviceID, record.Timestamp.Format(time.RFC3339))
					doc := client.Collection("sensor_data").Doc(docID)
					batch.Set(doc, record)
					log.Printf("Added document to batch: %s with data: %v", docID, record)
				}
			}

			if err := os.Remove(filePath); err != nil {
				log.Printf("Error deleting file %s: %v", file.Name(), err)
			} else {
				log.Printf("Deleted file after upload: %s", file.Name())
			}
		}
	}

	batch.End()

}

func messageHandler(client mqtt.Client, msg mqtt.Message) {
	log.Printf("Message %v beign handled", string(msg.Payload()))
	topic := msg.Topic()
	deviceID := topic[len("aqua/devices/") : len(topic)-len("/sensors/database")]

	payload := string(msg.Payload())

	data := SensorData{
		DeviceID:  deviceID,
		Data:      payload,
		Timestamp: time.Now(),
	}
	addToBuffer(data)
	saveToFile()
}

func subscribeToMQTT() {

	opts := mqtt.NewClientOptions().
		AddBroker("mqtt://test.mosquitto.org:1883").
		SetClientID("mqtt-subscriber").
		SetKeepAlive(60 * time.Second).            // Keep the connection alive every 60 seconds
		SetPingTimeout(10 * time.Second).          // Set a ping timeout for the server to respond
		SetAutoReconnect(true).                    // Enable auto-reconnect on failure
		SetMaxReconnectInterval(30 * time.Second). // Maximum interval for reconnecting attempts
		SetConnectionLostHandler(func(client mqtt.Client, err error) {
			log.Printf("MQTT connection lost: %v", err)
		}).
		SetOnConnectHandler(func(client mqtt.Client) {
			log.Println("MQTT reconnected successfully")
		})

	// Create a new MQTT client
	client := mqtt.NewClient(opts)

	// Try to connect to the MQTT broker
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Printf("Error connecting to MQTT: %v", token.Error())
		return
	}

	// Subscribe to the topic once connected
	topic := "aqua/devices/+/sensors/database"
	if token := client.Subscribe(topic, 1, messageHandler); token.Wait() && token.Error() != nil {
		log.Printf("Error subscribing to topic: %v", token.Error())
		return
	}

	log.Println("Subscribed to MQTT topic:", topic)

}

func scheduleDailyUpload() {
	for {
		now := time.Now()
		nextUpload := time.Date(now.Year(), now.Month(), now.Day(), 23, 19, 0, 0, now.Location())
		if now.After(nextUpload) {
			nextUpload = nextUpload.Add(24 * time.Hour)
		}
		time.Sleep(time.Until(nextUpload))

		log.Println("Scheduled upload triggered.")
		uploadToFirebase()
	}
}

func main() {
	err := godotenv.Load(".env")
	if err != nil {
		log.Println(err)
	}
	log.Println("Starting Aqua Gateway...")
	initFirebase()
	go subscribeToMQTT()
	go scheduleDailyUpload()

	select {} // Keep the program running
}
