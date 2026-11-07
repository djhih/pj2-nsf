package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

/*
customDb: a MongoDB implementation of the DbIf interface.

Every key-value pair is stored as one document in the "nsf.services" collection:

	{ _id: <key>, value: <value> }

Using the key as _id makes it unique, and Exist/Load/Remove can use the primary index.

The configuration is hardcoded here, since the customDb is not tested in TA's environment.
Start a MongoDB for it with:

	docker run -d --name nsf-mongo -p 127.0.0.1:27017:27017 mongo:latest
*/

const (
	mongoDbUri       = "mongodb://127.0.0.1:27017"
	mongoDbName       = "nsf"
	mongoDbCollection = "services"
	mongoDbTimeout    = 5 * time.Second
)

type mongoDocument struct {
	Key   string `bson:"_id"`
	Value string `bson:"value"`
}

type mongoDb struct {
	client     *mongo.Client
	collection *mongo.Collection
}

func newMongoDb(uri string) (*mongoDb, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetServerSelectionTimeout(mongoDbTimeout))
	if err != nil {
		return nil, fmt.Errorf("failed to create MongoDB client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), mongoDbTimeout)
	defer cancel()
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("failed to connect to MongoDB at %s: %v", uri, err)
	}

	return &mongoDb{
		client:     client,
		collection: client.Database(mongoDbName).Collection(mongoDbCollection),
	}, nil
}

func (m *mongoDb) Release() error {
	if m.client == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), mongoDbTimeout)
	defer cancel()
	return m.client.Disconnect(ctx)
}

func (m *mongoDb) Exist(key string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mongoDbTimeout)
	defer cancel()

	count, err := m.collection.CountDocuments(ctx, bson.D{{Key: "_id", Value: key}}, options.Count().SetLimit(1))
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (m *mongoDb) Save(key, value string) error {
	ctx, cancel := context.WithTimeout(context.Background(), mongoDbTimeout)
	defer cancel()

	// Upsert: insert the document, or replace it if the key already exists.
	_, err := m.collection.ReplaceOne(ctx,
		bson.D{{Key: "_id", Value: key}},
		mongoDocument{Key: key, Value: value},
		options.Replace().SetUpsert(true),
	)
	return err
}

func (m *mongoDb) SaveAll(data map[string]string) error {
	if len(data) == 0 {
		return nil
	}

	models := make([]mongo.WriteModel, 0, len(data))
	for key, value := range data {
		models = append(models, mongo.NewReplaceOneModel().
			SetFilter(bson.D{{Key: "_id", Value: key}}).
			SetReplacement(mongoDocument{Key: key, Value: value}).
			SetUpsert(true))
	}

	ctx, cancel := context.WithTimeout(context.Background(), mongoDbTimeout)
	defer cancel()

	_, err := m.collection.BulkWrite(ctx, models)
	return err
}

// Load returns an empty string without error when the key does not exist, the same as bboltDb.
func (m *mongoDb) Load(key string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mongoDbTimeout)
	defer cancel()

	var doc mongoDocument
	err := m.collection.FindOne(ctx, bson.D{{Key: "_id", Value: key}}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return doc.Value, nil
}

func (m *mongoDb) LoadAll() (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mongoDbTimeout)
	defer cancel()

	cursor, err := m.collection.Find(ctx, bson.D{})
	if err != nil {
		return nil, err
	}

	var docs []mongoDocument
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	result := make(map[string]string, len(docs))
	for _, doc := range docs {
		result[doc.Key] = doc.Value
	}
	return result, nil
}

func (m *mongoDb) Remove(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), mongoDbTimeout)
	defer cancel()

	_, err := m.collection.DeleteOne(ctx, bson.D{{Key: "_id", Value: key}})
	return err
}

func (m *mongoDb) RemoveAll() error {
	ctx, cancel := context.WithTimeout(context.Background(), mongoDbTimeout)
	defer cancel()

	_, err := m.collection.DeleteMany(ctx, bson.D{})
	return err
}
