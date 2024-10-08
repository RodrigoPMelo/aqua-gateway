package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"firebase.google.com/go"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/joho/godotenv"
	"google.golang.org/api/option"
)

type AggregatedData struct {
	AverageTemperature float64   `json:"average_temperature"`
	AverageTurbidity   float64   `json:"average_turbidity"`
	DeviceID           string    `json:"device_id"`
	Date               time.Time `json:"date"`
}

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

	fileName := fmt.Sprintf(dataPath+"/sensor_data_%s.json", time.Now().Format("2006-01-02"))
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

func processAndUploadDataToFirebase() {
	dataPath := os.Getenv("DATA_PATH")
	fileName := fmt.Sprintf("%s/sensor_data_%s.json", dataPath, time.Now().Format("2006-01-02"))

	// Verifica se o arquivo do dia existe
	if _, err := os.Stat(fileName); os.IsNotExist(err) {
		log.Printf("No data file found for today: %s", fileName)
		return
	}

	data, err := os.ReadFile(fileName)
	if err != nil {
		log.Printf("Error reading file %s: %v", fileName, err)
		return
	}

	var sensorData map[string][]SensorData
	if err := json.Unmarshal(data, &sensorData); err != nil {
		log.Printf("Error unmarshaling JSON for file %s: %v", fileName, err)
		return
	}

	// Preparação para agregar os dados
	aggregatedData := make(map[string]*AggregatedData)

	// Itera sobre os dados para calcular as médias
	for deviceID, records := range sensorData {
		var totalTemperature, totalTurbidity float64
		var count int

		for _, record := range records {
			turbidez, temperature, err := parseData(record.Data)
			if err != nil {
				log.Printf("Error parsing record data: %v", err)
				continue
			}

			totalTemperature += temperature
			totalTurbidity += turbidez
			count++
		}

		if count > 0 {
			averageTemperature := totalTemperature / float64(count)
			averageTurbidity := totalTurbidity / float64(count)

			aggregatedData[deviceID] = &AggregatedData{
				AverageTemperature: averageTemperature,
				AverageTurbidity:   averageTurbidity,
				DeviceID:           deviceID,
				Date:               time.Now(),
			}
		}
	}

	// Upload dos dados agregados ao Firebase
	ctx := context.Background()
	batch := client.BulkWriter(ctx)

	for deviceID, aggData := range aggregatedData {
		docID := fmt.Sprintf("%s_%s", deviceID, aggData.Date.Format("2006-01-02"))
		doc := client.Collection("sensor_data_aggregated").Doc(docID)
		batch.Set(doc, aggData)
		log.Printf("Added aggregated document to batch: %s with data: %v", docID, aggData)
	}

	batch.End()

	// Remove o arquivo após upload
	if err := os.Remove(fileName); err != nil {
		log.Printf("Error deleting file %s: %v", fileName, err)
	} else {
		log.Printf("Deleted file after upload: %s", fileName)
	}
}


func parseData(data string) (float64, float64, error) {
	// Regex para capturar os valores de turbidez e temperatura
	re := regexp.MustCompile(`turbidez: ([\d.]+), temperature: ([\d.]+)`)
	matches := re.FindStringSubmatch(data)

	if len(matches) < 3 {
		return 0, 0, fmt.Errorf("could not parse data: %s", data)
	}

	turbidez, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, 0, fmt.Errorf("error parsing turbidez: %v", err)
	}

	temperature, err := strconv.ParseFloat(matches[2], 64)
	if err != nil {
		return 0, 0, fmt.Errorf("error parsing temperature: %v", err)
	}

	return turbidez, temperature, nil
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
		nextUpload := time.Date(now.Year(), now.Month(), now.Day(), 23, 30, 0, 0, now.Location())
		if now.After(nextUpload) {
			nextUpload = nextUpload.Add(24 * time.Hour)
		}
		time.Sleep(time.Until(nextUpload))

		log.Println("Scheduled upload triggered.")
		processAndUploadDataToFirebase()
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
