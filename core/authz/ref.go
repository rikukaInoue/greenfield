package authz

// UserRef は user:<sub> 形式のsubject表記を返す。
func UserRef(subject string) string { return "user:" + subject }

// ServiceRef は service:<client_id> 形式のsubject表記を返す。
func ServiceRef(clientID string) string { return "service:" + clientID }

// ObjectRef は <type>:<id> 形式のobject表記を返す。
func ObjectRef(resourceType, id string) string { return resourceType + ":" + id }
