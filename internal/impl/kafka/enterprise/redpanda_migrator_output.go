// Copyright 2024 Redpanda Data, Inc.
//
// Licensed as a Redpanda Enterprise file under the Redpanda Community
// License (the "License"); you may not use this file except in compliance with
// the License. You may obtain a copy of the License at
//
// https://github.com/redpanda-data/connect/blob/main/licenses/rcl.md

package enterprise

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/twmb/franz-go/pkg/kgo"
	franz_sr "github.com/twmb/franz-go/pkg/sr"

	"github.com/redpanda-data/benthos/v4/public/service"

	"github.com/redpanda-data/connect/v4/internal/impl/confluent/sr"
	"github.com/redpanda-data/connect/v4/internal/impl/kafka"
	"github.com/redpanda-data/connect/v4/internal/license"
)

const (
	rmoFieldMaxInFlight                  = "max_in_flight"
	rmoFieldBatching                     = "batching"
	rmoFieldInputResource                = "input_resource"
	rmoFieldRepFactorOverride            = "replication_factor_override"
	rmoFieldRepFactor                    = "replication_factor"
	rmoFieldTranslateSchemaIDs           = "translate_schema_ids"
	rmoFieldSchemaRegistryOutputResource = "schema_registry_output_resource"

	// Deprecated
	rmoFieldRackID = "rack_id"

	rmoResourceDefaultLabel = "redpanda_migrator_output"
)

func redpandaMigratorOutputConfig() *service.ConfigSpec {
	return service.NewConfigSpec().
		Beta().
		Categories("Services").
		Version("4.37.0").
		Summary("A Redpanda Migrator output using the https://github.com/twmb/franz-go[Franz Kafka client library^].").
		Description(`
Writes a batch of messages to a Kafka broker and waits for acknowledgement before propagating it back to the input.

This output should be used in combination with a `+"`redpanda_migrator`"+` input which it can query for topic and ACL configurations.

If the configured broker does not contain the current message `+"topic"+`, it attempts to create it along with the topic
ACLs which are read automatically from the `+"`redpanda_migrator`"+` input identified by the label specified in
`+"`input_resource`"+`.

ACL migration adheres to the following principles:

- `+"`ALLOW WRITE`"+` ACLs for topics are not migrated
- `+"`ALLOW ALL`"+` ACLs for topics are downgraded to `+"`ALLOW READ`"+`
- Only topic ACLs are migrated, group ACLs are not migrated
`).
		Fields(redpandaMigratorOutputConfigFields()...).
		LintRule(kafka.FranzWriterConfigLints()).
		Example("Transfer data", "Writes messages to the configured broker and creates topics and topic ACLs if they don't exist. It also ensures that the message order is preserved.", `
output:
  redpanda_migrator:
    seed_brokers: [ "127.0.0.1:9093" ]
    topic: ${! metadata("kafka_topic").or(throw("missing kafka_topic metadata")) }
    key: ${! metadata("kafka_key") }
    partitioner: manual
    partition: ${! metadata("kafka_partition").or(throw("missing kafka_partition metadata")) }
    timestamp_ms: ${! metadata("kafka_timestamp_ms").or(timestamp_unix_milli()) }
    input_resource: redpanda_migrator_input
    max_in_flight: 1
`)
}

func redpandaMigratorOutputConfigFields() []*service.ConfigField {
	return slices.Concat(
		kafka.FranzConnectionFields(),
		kafka.FranzWriterConfigFields(),
		[]*service.ConfigField{
			service.NewIntField(rmoFieldMaxInFlight).
				Description("The maximum number of batches to be sending in parallel at any given time.").
				Default(256),
			service.NewStringField(rmoFieldInputResource).
				Description("The label of the redpanda_migrator input from which to read the configurations for topics and ACLs which need to be created.").
				Default(rmiResourceDefaultLabel).
				Advanced(),
			service.NewBoolField(rmoFieldRepFactorOverride).
				Description("Use the specified replication factor when creating topics.").
				Default(true).
				Advanced(),
			service.NewIntField(rmoFieldRepFactor).
				Description("Replication factor for created topics. This is only used when `replication_factor_override` is set to `true`.").
				Default(3).
				Advanced(),
			service.NewBoolField(rmoFieldTranslateSchemaIDs).Description("Translate schema IDs.").Default(true).Advanced(),
			service.NewStringField(rmoFieldSchemaRegistryOutputResource).
				Description("The label of the schema_registry output to use for fetching schema IDs.").
				Default(sroResourceDefaultLabel).
				Advanced(),

			// Deprecated
			service.NewStringField(rmoFieldRackID).Deprecated(),
			service.NewBatchPolicyField(rmoFieldBatching).Deprecated(),
		},
		kafka.FranzProducerFields(),
	)
}

func init() {
	err := service.RegisterBatchOutput("redpanda_migrator", redpandaMigratorOutputConfig(),
		func(conf *service.ParsedConfig, mgr *service.Resources) (
			output service.BatchOutput,
			batchPolicy service.BatchPolicy,
			maxInFlight int,
			err error,
		) {
			if err = license.CheckRunningEnterprise(mgr); err != nil {
				return
			}

			if maxInFlight, err = conf.FieldInt(rmoFieldMaxInFlight); err != nil {
				return
			}

			var inputResource string
			if inputResource, err = conf.FieldString(rmoFieldInputResource); err != nil {
				return
			}

			var replicationFactorOverride bool
			if replicationFactorOverride, err = conf.FieldBool(rmoFieldRepFactorOverride); err != nil {
				return
			}

			var replicationFactor int
			if replicationFactor, err = conf.FieldInt(rmoFieldRepFactor); err != nil {
				return
			}

			var translateSchemaIDs bool
			if translateSchemaIDs, err = conf.FieldBool(rmoFieldTranslateSchemaIDs); err != nil {
				return
			}

			var schemaRegistryOutputResource srResourceKey
			if translateSchemaIDs {
				var res string
				if res, err = conf.FieldString(rmoFieldSchemaRegistryOutputResource); err != nil {
					return
				}
				schemaRegistryOutputResource = srResourceKey(res)
			}

			var clientLabel string
			if clientLabel = mgr.Label(); clientLabel == "" {
				clientLabel = rmoResourceDefaultLabel
			}

			var tmpOpts, clientOpts []kgo.Opt

			var connDetails *kafka.FranzConnectionDetails
			if connDetails, err = kafka.FranzConnectionDetailsFromConfig(conf, mgr.Logger()); err != nil {
				return
			}
			clientOpts = append(clientOpts, connDetails.FranzOpts()...)

			if tmpOpts, err = kafka.FranzProducerOptsFromConfig(conf); err != nil {
				return
			}
			clientOpts = append(clientOpts, tmpOpts...)

			clientOpts = append(clientOpts, kgo.AllowAutoTopicCreation()) // TODO: Configure this?

			var client *kgo.Client
			var clientMut sync.Mutex
			// Stores the source to destination SchemaID mapping.
			var schemaIDCache sync.Map
			var topicCache sync.Map
			output, err = kafka.NewFranzWriterFromConfig(conf,
				func(fn kafka.FranzSharedClientUseFn) error {
					clientMut.Lock()
					defer clientMut.Unlock()

					if client == nil {
						var err error
						if client, err = kgo.NewClient(clientOpts...); err != nil {
							return err
						}
					}

					return fn(&kafka.FranzSharedClientInfo{
						Client:      client,
						ConnDetails: connDetails,
					})
				},
				func(context.Context) error {
					clientMut.Lock()
					defer clientMut.Unlock()

					if client == nil {
						return nil
					}

					_, _ = kafka.FranzSharedClientPop(clientLabel, mgr)

					client.Close()
					client = nil
					return nil
				},
				func(client *kgo.Client) {
					if err = kafka.FranzSharedClientSet(clientLabel, &kafka.FranzSharedClientInfo{
						Client: client,
					}, mgr); err != nil {
						mgr.Logger().With("error", err).Warn("Failed to store client connection for sharing")
					}
				},
				func(ctx context.Context, client *kgo.Client, records []*kgo.Record) error {
					if translateSchemaIDs {

						if res, ok := mgr.GetGeneric(schemaRegistryOutputResource); ok {
							srOutput := res.(*schemaRegistryOutput)

							var ch franz_sr.ConfluentHeader
							for recordIdx, record := range records {
								schemaID, _, err := ch.DecodeID(record.Value)
								if err != nil {
									mgr.Logger().Warnf("Failed to extract schema ID from message index %d on topic %q: %s", recordIdx, record.Topic, err)
									continue
								}

								var destSchemaID int
								if cachedID, ok := schemaIDCache.Load(schemaID); !ok {
									destSchemaID, err = srOutput.GetDestinationSchemaID(ctx, schemaID)
									if err != nil {
										mgr.Logger().Warnf("Failed to fetch destination schema ID from message index %d on topic %q: %s", recordIdx, record.Topic, err)
										continue
									}
									schemaIDCache.Store(schemaID, destSchemaID)
								} else {
									destSchemaID = cachedID.(int)
								}

								err = sr.UpdateID(record.Value, destSchemaID)
								if err != nil {
									mgr.Logger().Warnf("Failed to update schema ID in message index %d on topic %s: %q", recordIdx, record.Topic, err)
									continue
								}
							}
						} else {
							mgr.Logger().Warnf("schema_registry output resource %q not found; skipping schema ID translation", schemaRegistryOutputResource)
							return nil
						}

					}

					// Once we get here, the input should already be initialised and its pre-flight hook should have
					// been called already. Thus, we don't need to loop until the input is ready.
					if err := kafka.FranzSharedClientUse(inputResource, mgr, func(details *kafka.FranzSharedClientInfo) error {
						for _, record := range records {
							if _, ok := topicCache.Load(record.Topic); !ok {
								if err := createTopic(ctx, record.Topic, replicationFactorOverride, replicationFactor, details.Client, client); err != nil && err != errTopicAlreadyExists {
									return fmt.Errorf("failed to create topic %q: %s", record.Topic, err)
								} else {
									if err == errTopicAlreadyExists {
										mgr.Logger().Debugf("Topic %q already exists", record.Topic)
									} else {
										mgr.Logger().Infof("Created topic %q", record.Topic)
									}
									if err := createACLs(ctx, record.Topic, details.Client, client); err != nil {
										mgr.Logger().Errorf("Failed to create ACLs for topic %q: %s", record.Topic, err)
									}

									topicCache.Store(record.Topic, struct{}{})
								}
							}
						}
						return nil
					}); err != nil {
						mgr.Logger().With("error", err, "resource", inputResource).Warn("Failed to access shared client for given resource identifier")
					}

					return nil
				})
			return
		})
	if err != nil {
		panic(err)
	}
}
