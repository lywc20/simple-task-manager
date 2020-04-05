package auth

import (
	"crypto/aes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"time"

	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/hauke96/sigolo"
	"github.com/kurrik/oauth1a"

	"../config"
	"../util"
)

// Struct for authentication
type Token struct {
	ValidUntil int64  `json:"valid_until"`
	User       string `json:"user"`
	Secret     string `json:"secret"`
}

var (
	oauthRedirectUrl  string
	oauthConsumerKey  string
	oauthSecret       string
	oauthBaseUrl      string
	osmUserDetailsUrl string

	service *oauth1a.Service

	configs          map[string]*oauth1a.UserConfig
	tokenSecretNonce [32]byte
)

func InitAuth() {
	oauthRedirectUrl = fmt.Sprintf("%s:%d/oauth_callback", config.Conf.ServerUrl, config.Conf.Port)
	oauthConsumerKey = config.Conf.OauthConsumerKey
	oauthSecret = config.Conf.OauthSecret
	oauthBaseUrl = config.Conf.OsmBaseUrl
	osmUserDetailsUrl = config.Conf.OsmBaseUrl + "/api/0.6/user/details"

	service = &oauth1a.Service{
		RequestURL:   config.Conf.OsmBaseUrl + "/oauth/request_token",
		AuthorizeURL: config.Conf.OsmBaseUrl + "/oauth/authorize",
		AccessURL:    config.Conf.OsmBaseUrl + "/oauth/access_token",
		ClientConfig: &oauth1a.ClientConfig{
			ConsumerKey:    oauthConsumerKey,
			ConsumerSecret: oauthSecret,
			CallbackURL:    oauthRedirectUrl,
		},
		Signer: new(oauth1a.HmacSha1Signer),
	}

	configs = make(map[string]*oauth1a.UserConfig)
	tokenSecretNonce = sha256.Sum256(getRandomBytes(265))
}

func OauthLogin(w http.ResponseWriter, r *http.Request) {
	userConfig := &oauth1a.UserConfig{}
	configKey := fmt.Sprintf("%x", sha256.Sum256(getRandomBytes(64)))

	service.ClientConfig.CallbackURL = oauthRedirectUrl + "?redirect=" + r.FormValue("redirect") + "&config=" + configKey
	sigolo.Info("%s", service.ClientConfig.CallbackURL)

	httpClient := new(http.Client)
	err := userConfig.GetRequestToken(service, httpClient)
	if err != nil {
		sigolo.Error(err.Error())
		return
	}

	url, err := userConfig.GetAuthorizeURL(service)
	if err != nil {
		sigolo.Error(err.Error())
		return
	}

	configs[configKey] = userConfig
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func OauthCallback(w http.ResponseWriter, r *http.Request) {
	sigolo.Info("Callback called")

	configKey, err := util.GetParam("config", r)
	if err != nil {
		util.ResponseBadRequest(w, err.Error())
		return
	}

	userConfig, ok := configs[configKey]
	if !ok || userConfig == nil {
		sigolo.Error("User config not found")
		return
	}
	configs[configKey] = nil

	clientRedirectUrl, err := util.GetParam("redirect", r)
	if err != nil {
		util.ResponseBadRequest(w, err.Error())
		return
	}

	err = requestAccessToken(r, userConfig)
	if err != nil {
		sigolo.Error(err.Error())
		return
	}

	userName, err := requestUserInformation(userConfig)
	if err != nil {
		sigolo.Error(err.Error())
		return
	}

	sigolo.Info("Create token for user '%s'", userName)

	tokenValidDuration, _ := time.ParseDuration("24h")
	validUntil := time.Now().Add(tokenValidDuration).Unix()

	secret, err := createSecret(userName, validUntil)
	if err != nil {
		sigolo.Error(err.Error())
		return
	}

	// Create actual token
	token := &Token{
		ValidUntil: validUntil,
		User:       userName,
		Secret:     secret,
	}

	jsonBytes, err := json.Marshal(token)
	if err != nil {
		sigolo.Error(err.Error())
		return
	}

	encodedTokenString := base64.StdEncoding.EncodeToString(jsonBytes)

	http.Redirect(w, r, clientRedirectUrl+"?token="+encodedTokenString, http.StatusTemporaryRedirect)
}

func requestAccessToken(r *http.Request, userConfig *oauth1a.UserConfig) error {
	token := r.FormValue("oauth_token")
	userConfig.AccessTokenSecret = token
	userConfig.Verifier = r.FormValue("oauth_verifier")

	httpClient := new(http.Client)
	return userConfig.GetAccessToken(userConfig.RequestTokenKey, userConfig.Verifier, service, httpClient)
}

func requestUserInformation(userConfig *oauth1a.UserConfig) (string, error) {
	req, err := http.NewRequest("GET", osmUserDetailsUrl, nil)
	if err != nil {
		sigolo.Error("Creating request user information failed: %s", err.Error())
		return "", err
	}

	err = service.Sign(req, userConfig)
	if err != nil {
		sigolo.Error("Signing request failed: %s", err.Error())
		return "", err
	}

	client := &http.Client{}
	response, err := client.Do(req)
	if err != nil {
		sigolo.Error("Requesting user information failed: %s", err.Error())
		return "", err
	}

	responseBody, err := ioutil.ReadAll(response.Body)
	defer response.Body.Close()
	if err != nil {
		sigolo.Error("Could not get response body: %s", err.Error())
		return "", err
	}

	var osm util.Osm
	xml.Unmarshal(responseBody, &osm)

	return osm.User.DisplayName, nil
}

// createSecret builds a new secret string encoded as base64. The idea: Take a
// secret string, hash it (so disguise the length of this secret) and encrypt it.
// To have equal length secrets, hash it again.
func createSecret(user string, validTime int64) (string, error) {
	key := sha256.Sum256([]byte("some very secret key"))
	secretBaseString := fmt.Sprintf("%x%s%d", tokenSecretNonce, user, validTime)
	secretHashedBytes := sha256.Sum256([]byte(secretBaseString))

	cipher, err := aes.NewCipher(key[:])
	if err != nil {
		sigolo.Error(err.Error())
		return "", err
	}

	secretEncryptedBytes := make([]byte, 32)
	cipher.Encrypt(secretEncryptedBytes, secretHashedBytes[:])

	secretEncryptedHashedBytes := sha256.Sum256([]byte(secretEncryptedBytes))

	return base64.StdEncoding.EncodeToString(secretEncryptedHashedBytes[:]), nil
}

func getRandomBytes(count int) []byte {
	bytes := make([]byte, count)
	rand.Read(bytes)
	return bytes
}

// verifyRequest checks the integrity of the token and the "valiUntil" date. It
// then returns the token but without the secret part, just the metainformation
// (e.g. user name) is set.
func VerifyRequest(r *http.Request) (*Token, error) {
	encodedToken := r.Header.Get("Authorization")

	tokenBytes, err := base64.StdEncoding.DecodeString(encodedToken)
	if err != nil {
		sigolo.Error(err.Error())
		return nil, err
	}

	var token Token
	err = json.Unmarshal(tokenBytes, &token)
	if err != nil {
		sigolo.Error(err.Error())
		return nil, err
	}

	targetSecret, err := createSecret(token.User, token.ValidUntil)
	if err != nil {
		sigolo.Error(err.Error())
		return nil, err
	}

	if token.Secret != targetSecret {
		return nil, errors.New("Secret not valid")
	}

	if token.ValidUntil < time.Now().Unix() {
		return nil, errors.New("Token expired")
	}

	sigolo.Debug("User '%s' has valid token", token.User)
	sigolo.Info("User '%s' called %s on %s", token.User, r.Method, r.URL.Path)

	token.Secret = ""
	return &token, nil
}