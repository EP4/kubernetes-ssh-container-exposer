package registry

import (
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
	"github.com/kelseyhightower/envconfig"
	"github.com/tg123/sshpiper/sshpiperd/upstream/mysql/crud"
	"go.uber.org/zap"
)

type Registrable interface {
	RegisterUpstream(upstream *Upstream) (*Upstream, error)
}

type Registry struct {
	logger   *zap.Logger
	database *sql.DB
}

type Config struct {
	Port     int    `default:"3306"`
	Host     string `default:"localhost"`
	User     string `default:"root"`
	Password string `default:""`
	Database string `default:"sshpiper"`
}

type Upstream struct {
	Name                string
	Username            string
	Address             string
	SSHPiperPrivateKey  string
	DownstreamPublicKey []string
}

func NewRegistry(logger *zap.Logger) *Registry {
	return &Registry{
		logger: logger,
	}
}

func (r *Registry) ConnectDatabase() error {
	var conf Config
	var err error
	err = envconfig.Process("KSCE_MYSQL", &conf)
	if err != nil {
		return err
	}
	r.logger.Info("MySQL Config", zap.String("user", conf.User), zap.String("host", conf.Host), zap.Int("port", conf.Port), zap.String("database", conf.Database))
	source := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", conf.User, conf.Password, conf.Host, conf.Port, conf.Database)
	r.database, err = sql.Open("mysql", source)
	return err
}

func (r *Registry) IsConnected() bool {
	return r.database != nil
}

func (r *Registry) truncate(table string, hasForeignKey bool, ignoreForeignKeyChecks bool) error {
	var tx *sql.Tx
	var err error
	if tx, err = r.database.Begin(); err != nil {
		return err
	}
	if hasForeignKey && ignoreForeignKeyChecks {
		if _, err = tx.Exec("set foreign_key_checks = 0;"); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(fmt.Sprintf("truncate table %s;", table)); err != nil {
		return err
	}
	if hasForeignKey && ignoreForeignKeyChecks {
		if _, err = tx.Exec("set foreign_key_checks = 1;"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Registry) TruncateAll() error {
	var err error
	type table struct {
		name          string
		hasForeignKey bool
	}
	tables := []table{
		{"pubkey_prikey_map", false},
		{"pubkey_upstream_map", false},
		{"user_upstream_map", false},
		{"private_keys", true},
		{"public_keys", true},
		{"server", true},
		{"upstream", true},
	}
	for _, table := range tables {
		if err = r.truncate(table.name, table.hasForeignKey, table.hasForeignKey); err != nil {
			return err
		}
	}
	r.logger.Info("Database truncated")
	return nil
}

func (r *Registry) RegisterUpstream(upstream *Upstream) (*Upstream, error) {
	var err error
	var serverID int64
	var upstreamID int64
	var privateKeyID int64
	var publicKeyID int64

	s := crud.NewServer(r.database)
	if rec, err := s.GetFirstByAddress(upstream.Address); err == nil {
		if rec != nil {
			serverID = rec.Id
		} else {
			if serverID, err = s.Post(&crud.ServerRecord{Name: upstream.Name, Address: upstream.Address}); err == nil {
				err = s.Commit()
			} else {
				err = s.Rollback()
			}
		}
	}
	if err != nil {
		return nil, err
	}

	u := crud.NewUpstream(r.database)
	if rec, err := u.GetFirstByServerId(serverID); err == nil {
		if rec != nil {
			upstreamID = rec.Id
		} else {
			if upstreamID, err = u.Post(&crud.UpstreamRecord{Name: upstream.Name, ServerId: serverID, Username: upstream.Name}); err == nil {
				err = u.Commit()
			} else {
				err = u.Rollback()
			}
		}
	}
	if err != nil {
		return nil, err
	}

	uum := crud.NewUserUpstreamMap(r.database)
	if rec, err := uum.GetFirstByUpstreamId(upstreamID); err == nil && rec == nil {
		if _, err = uum.Post(&crud.UserUpstreamMapRecord{UpstreamId: upstreamID, Username: upstream.Username}); err == nil {
			err = uum.Commit()
		} else {
			err = uum.Rollback()
		}
	}
	if err != nil {
		return nil, err
	}

	prv := crud.NewPrivateKeys(r.database)
	if rec, err := prv.GetFirstByName(upstream.Name); err == nil {
		if rec != nil {
			privateKeyID = rec.Id
		} else {
			if privateKeyID, err = prv.Post(&crud.PrivateKeysRecord{Name: upstream.Name, Data: upstream.SSHPiperPrivateKey}); err == nil {
				err = prv.Commit()
			} else {
				err = prv.Rollback()
			}
		}
	}
	if err != nil {
		return nil, err
	}

	for i := range upstream.DownstreamPublicKey {
		r.logger.Info("New DownstreamPublicKey")
		pub := crud.NewPublicKeys(r.database)
		// if rec, err := pub.GetFirstByName(upstream.Name); err == nil {
		// if rec != nil {
		//	publicKeyID = rec.Id
		//	logger.Info("Key Fetched")
		// } else {
		if publicKeyID, err = pub.Post(&crud.PublicKeysRecord{Name: upstream.Name, Data: upstream.DownstreamPublicKey[i]}); err == nil {
			err = pub.Commit()
			r.logger.Info("Key Added")
			r.logger.Info(upstream.DownstreamPublicKey[i])
		} else {
			err = pub.Rollback()
		}
		// }
		// }
		if err != nil {
			return nil, err
		}

		ppm := crud.NewPubkeyPrikeyMap(r.database)
		// if rec, err := ppm.GetFirstByPrivateKeyId(privateKeyID); err == nil && rec == nil {
		if _, err = ppm.Post(&crud.PubkeyPrikeyMapRecord{PrivateKeyId: privateKeyID, PubkeyId: publicKeyID}); err == nil {
			err = ppm.Commit()
			r.logger.Info("Key Map Added")
		} else {
			err = ppm.Rollback()
		}
		// }
		// if err != nil {
		// 	return nil, err
		// }
		r.logger.Info("########")
	}

	r.logger.Info("Upstream registered", zap.String("name", upstream.Name), zap.String("username", upstream.Username))

	return nil, err
}