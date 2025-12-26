package models

type Item struct {
	ID            int64  `yaml:"id"`
	Name          string `yaml:"name"`
	Description   string `yaml:"description"`
	TotalQuantity int64  `yaml:"total_quantity"`
	Order         int    `yaml:"order" json:"order"`
}
