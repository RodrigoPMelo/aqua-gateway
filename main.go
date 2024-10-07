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
		log.Fatalf("error initializing app: %v", err)
	}
	client, err = app.Firestore(ctx)
	if err != nil {
		log.Fatalf("error initializing Firestore: %v", err)
	}
}

func addToBuffer(data SensorData) {
	dataMutex.Lock()
	defer dataMutex.Unlock()

	// Adiciona os dados ao buffer
	deviceID := data.DeviceID
	dataBuffer[deviceID] = append(dataBuffer[deviceID], data)
}

func saveBufferToFile() {
	dataMutex.Lock()
	defer dataMutex.Unlock()

	// Salva o buffer inteiro em um único arquivo
	fileName := fmt.Sprintf("/data/sensor_data_%s.json", time.Now().Format("2006-01-02"))
	fileData, _ := json.Marshal(dataBuffer)
	err := os.WriteFile(fileName, fileData, 0644)
	if err != nil {
		log.Printf("Error saving buffer to file: %v", err)
	}
}

func uploadToFirebase() {
	// Lê os arquivos do diretório /data/
	entries, err := os.ReadDir("/data/")
	if err != nil {
		log.Printf("Error reading directory: %v", err)
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

	// Inicializa o Firestore e o Batch
	ctx := context.Background()
	client, err := firestore.NewClient(ctx, "your-project-id") // Substitua "your-project-id"
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer client.Close()

	batch := client.BulkWriter(ctx)

	// Itera sobre os arquivos do diretório
	for _, file := range infos {
		if !file.IsDir() {
			filePath := "/data/" + file.Name()
			
			// Lê o conteúdo do arquivo
			data, err := os.ReadFile(filePath)
			if err != nil {
				log.Printf("Error reading file %s: %v", file.Name(), err)
				continue
			}

			// Desserializa o JSON no formato de sensorData
			var sensorData map[string][]SensorData
			if err := json.Unmarshal(data, &sensorData); err != nil {
				log.Printf("Error unmarshaling JSON for file %s: %v", file.Name(), err)
				continue
			}

			// Adiciona ao batch
			for deviceID, records := range sensorData {
				for _, record := range records {
					docID := fmt.Sprintf("%s_%s", deviceID, record.Timestamp.Format(time.RFC3339))
					doc := client.Collection("sensor_data").Doc(docID)

					// Adiciona o documento ao batch
					batch.Set(doc, record)
				}
			}

			// Apaga o arquivo após o upload
			if err := os.Remove(filePath); err != nil {
				log.Printf("Error deleting file %s: %v", file.Name(), err)
			}
		}
	}

	// Commit do batch (envio dos dados)
	batch.End()
}


func messageHandler(client mqtt.Client, msg mqtt.Message) {
	topic := msg.Topic()
	deviceID := topic[len("aqua/devices/") : len(topic)-len("/sensors")]

	var payload string
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		log.Printf("Error parsing payload: %v", err)
		return
	}

	data := SensorData{
		DeviceID:  deviceID,
		Data:    payload,
		Timestamp: time.Now(),
	}
	addToBuffer(data)
}

func subscribeToMQTT() {
	opts := mqtt.NewClientOptions().AddBroker("tcp://mqtt-broker:1883").SetClientID("mqtt-subscriber")
	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("Error connecting to MQTT: %v", token.Error())
	}

	topic := "aqua/devices/+/sensors"
	if token := client.Subscribe(topic, 1, messageHandler); token.Wait() && token.Error() != nil {
		log.Fatalf("Error subscribing to topic: %v", token.Error())
	}
}

func scheduleDailyUpload() {
	for {
		now := time.Now()
		nextUpload := time.Date(now.Year(), now.Month(), now.Day(), 23, 0, 0, 0, now.Location())
		if now.After(nextUpload) {
			nextUpload = nextUpload.Add(24 * time.Hour)
		}
		time.Sleep(time.Until(nextUpload))

		// Salva os dados do buffer em arquivo e depois faz o upload
		saveBufferToFile()
		uploadToFirebase()
	}
}

func main() {
	initFirebase()
	go subscribeToMQTT()
	go scheduleDailyUpload()

	select {} // Mantém o programa em execução
}
