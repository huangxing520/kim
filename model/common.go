package model
type KafkaSettings struct {
	Enable            bool
	Brokers           []string
	Topic             string
	BufferSize        int
	Timeout           string
	ReplicationFactor int
	Partitions        int
}