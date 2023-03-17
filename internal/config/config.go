package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

type Duration struct {
	time.Duration
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		d.Duration = time.Duration(value)
		return nil
	case string:
		var err error
		d.Duration, err = time.ParseDuration(value)
		if err != nil {
			return err
		}
		return nil
	default:
		return errors.New("invalid duration")
	}
}

type Configuration struct {
	Mail struct {
		Server string `json:"server"`
		Port   int    `json:"port"`
		From   struct {
			Name string `json:"name"`
			Mail string `json:"mail"`
		} `json:"from"`
		User     string `json:"user"`
		Password string `json:"password"`
		SkipTLS  bool   `json:"skiptls"`
	} `json:"mail"`
	Templates struct {
		HTML string `json:"html"`
		TXT  string `json:"txt"`
	} `json:"templates"`
	Subject string   `json:"subject"`
	Timeout Duration `json:"timeout"`
}

func GetConfig(f string) (Configuration, error) {
	if f == "" {
		return Configuration{}, fmt.Errorf("please provide a valid config file")
	}

	b, err := os.ReadFile(f) // nolint: gosec
	if err != nil {
		return Configuration{}, err
	}
	reader := bytes.NewReader(b)

	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()

	c := Configuration{}
	if err = decoder.Decode(&c); err != nil {
		var syntaxErr *json.SyntaxError
		var unmarshalErr *json.UnmarshalTypeError
		switch {
		case errors.As(err, &syntaxErr):
			custom := fmt.Sprintf("%q <-", string(b[syntaxErr.Offset-20:syntaxErr.Offset]))
			return Configuration{}, fmt.Errorf("could not parse JSON: %v: %s", syntaxErr.Error(), custom)
		case errors.As(err, &unmarshalErr):
			custom := fmt.Sprintf("%q <-", string(b[unmarshalErr.Offset-20:unmarshalErr.Offset]))
			return Configuration{}, fmt.Errorf("could not parse JSON: type %v cannot be converted into %v (%s.%v): %v: %s", unmarshalErr.Value, unmarshalErr.Type.Name(), unmarshalErr.Struct, unmarshalErr.Field, unmarshalErr.Error(), custom)
		default:
			return Configuration{}, err
		}
	}

	return c, nil
}
