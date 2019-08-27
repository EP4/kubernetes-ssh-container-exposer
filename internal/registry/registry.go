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
	UnregisterUpstream(upstream *Upstream) error
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

func (r *Registry) UnregisterUpstream(upstream *Upstream) error {
	// note the get{x}Record functions are expected to return values which make it safe
	// to assume not nil on the record after checking the err is not nil. This allows us to reduce
	// the verbosity of the code here. These functions are also expected to set sentinel errors from sql
	// for no rows found so that the caller can make informed decisions on how they will handle the failure.

	s, serverRec, err := r.getServerRecord(upstream.Address)
	if err != nil {
		return err
	}

	u, upstreamRec, err := r.getUpstreamRecord(serverRec.Id)
	if err != nil {
		return err
	}

	um, upstreamUserMap, err := r.getUpstreamUserMapRecord(upstreamRec.Id)
	if err != nil {
		return err
	}

	// respect constraints and delete in reverse
	err = r.deleteUpstreamUserMapRecord(um, upstreamUserMap)
	if err != nil {
		return err
	}

	err = r.deleteUpstreamRecord(u, upstreamRec)
	if err != nil {
		return err
	}

	err = r.deleteServerRecord(s, serverRec)
	if err != nil {
		return err
	}

	// cleaning up keys and mappings
	privateKey, pkRec, err := r.getPrivateKeys(upstream.Name)
	if err != nil {
		return err
	}

	pubPrivKeyMap, ppkRec, err := r.getPublicPrivateKeyMap(pkRec.Id)
	if err != nil {
		return err
	}

	// get the list of public keys associated with the name
	publicKeys, pubKeyRec, err := r.getPublicKeys(upstream.Name)
	if err != nil {
		return err
	}

	errs := r.deletePublicPrivateKeyMap(pubPrivKeyMap, ppkRec)
	if len(errs) > 0 {
		for _, e := range errs {
			r.logger.Sugar().Errorf("error deleting entry from 'pubkey_prikey_map' - %v", e)
		}
		return fmt.Errorf("failed to delete some required entries")
	}

	errs = r.deletePublicKeyRecords(publicKeys, pubKeyRec)
	if len(errs) > 0 {
		for _, e := range errs {
			r.logger.Sugar().Errorf("error deleting entry from 'public_keys' - %v", e)
		}
		return fmt.Errorf("failed to delete some required entries")
	}

	err = r.deletePrivateKeyRecord(privateKey, pkRec)
	if err != nil {
		return err
	}

	r.logger.Info("Upstream unregistered", zap.String("name", upstream.Name), zap.String("username", upstream.Username))
	return nil
}

func (r *Registry) getServerRecord(address string) (*crud.Server, *crud.ServerRecord, error) {
	s := crud.NewServer(r.database)
	rec, err := s.GetFirstByAddress(address)
	if err != nil {
		return s, nil, err
	}
	if rec == nil {
		err = sql.ErrNoRows
		return s, nil, err
	}
	return s, rec, nil
}

func (r *Registry) getUpstreamRecord(serverID int64) (*crud.Upstream, *crud.UpstreamRecord, error) {
	u := crud.NewUpstream(r.database)
	rec, err := u.GetFirstByServerId(serverID)
	if err != nil {
		return u, nil, err
	}
	if rec == nil {
		err = sql.ErrNoRows
		return u, nil, err
	}
	return u, rec, nil
}

func (r *Registry) getUpstreamUserMapRecord(upstreamID int64) (*crud.UserUpstreamMap, *crud.UserUpstreamMapRecord, error) {
	u := crud.NewUserUpstreamMap(r.database)
	rec, err := u.GetFirstByUpstreamId(upstreamID)
	if err != nil {
		return u, nil, err
	}
	if rec == nil {
		err = sql.ErrNoRows
		return u, nil, err
	}
	return u, rec, nil
}

func (r *Registry) getPublicKeys(upstream string) (*crud.PublicKeys, []*crud.PublicKeysRecord, error) {
	pk := crud.NewPublicKeys(r.database)
	rec, err := pk.GetByName(upstream)
	if err != nil {
		return pk, nil, err
	}
	if rec == nil {
		err = sql.ErrNoRows
		return pk, nil, err
	}
	return pk, rec, nil
}

func (r *Registry) getPrivateKeys(upstream string) (*crud.PrivateKeys, *crud.PrivateKeysRecord, error) {
	pk := crud.NewPrivateKeys(r.database)
	rec, err := pk.GetFirstByName(upstream)
	if err != nil {
		return pk, nil, err
	}
	if rec == nil {
		err = sql.ErrNoRows
		return pk, nil, err
	}
	return pk, rec, nil
}

func (r *Registry) getPublicPrivateKeyMap(privateKeyID int64) (*crud.PubkeyPrikeyMap, []*crud.PubkeyPrikeyMapRecord, error) {
	ppk := crud.NewPubkeyPrikeyMap(r.database)
	rec, err := ppk.GetByPrivateKeyId(privateKeyID)
	if err != nil {
		return ppk, nil, err
	}
	if rec == nil {
		err = sql.ErrNoRows
		return ppk, nil, err
	}
	return ppk, rec, nil
}

func (r *Registry) deleteServerRecord(s *crud.Server, rec *crud.ServerRecord) error {
	_, err := s.Delete(rec)
	if err != nil {
		return err
	}
	err = s.Commit()
	if err != nil {
		rbErr := s.Rollback()
		r.logger.Sugar().Errorf("error during rollback %v", rbErr)
	}
	return err
}

func (r *Registry) deleteUpstreamRecord(u *crud.Upstream, rec *crud.UpstreamRecord) error {
	_, err := u.Delete(rec)
	if err != nil {
		return err
	}
	err = u.Commit()
	if err != nil {
		rbErr := u.Rollback()
		r.logger.Sugar().Errorf("error during rollback %v", rbErr)
	}
	return err
}

func (r *Registry) deleteUpstreamUserMapRecord(u *crud.UserUpstreamMap, rec *crud.UserUpstreamMapRecord) error {
	_, err := u.Delete(rec)
	if err != nil {
		return err
	}
	err = u.Commit()
	if err != nil {
		rbErr := u.Rollback()
		r.logger.Sugar().Errorf("error during rollback %v", rbErr)
	}
	return err
}

func (r *Registry) deletePrivateKeyRecord(u *crud.PrivateKeys, rec *crud.PrivateKeysRecord) error {
	_, err := u.Delete(rec)
	if err != nil {
		return err
	}
	err = u.Commit()
	if err != nil {
		rbErr := u.Rollback()
		r.logger.Sugar().Errorf("error during rollback %v", rbErr)
	}
	return err
}

func (r *Registry) deletePublicKeyRecords(u *crud.PublicKeys, rec []*crud.PublicKeysRecord) []error {
	var errs []error
	for _, record := range rec {
		_, err := u.Delete(record)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		err = u.Commit()
		if err != nil {
			rbErr := u.Rollback()
			r.logger.Sugar().Errorf("error during rollback %v", rbErr)
			errs = append(errs, rbErr)
			continue
		}
	}
	return errs
}

func (r *Registry) deletePublicPrivateKeyMap(u *crud.PubkeyPrikeyMap, rec []*crud.PubkeyPrikeyMapRecord) []error {
	var errs []error
	for _, record := range rec {
		_, err := u.Delete(record)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		err = u.Commit()
		if err != nil {
			rbErr := u.Rollback()
			r.logger.Sugar().Errorf("error during rollback %v", rbErr)
			errs = append(errs, rbErr)
			continue
		}
	}
	return errs
}
