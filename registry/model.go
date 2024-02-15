package registry

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	log "github.com/duglin/dlog"
)

var RegexpPropName = regexp.MustCompile("^[a-z_][a-z0-9_./]{0,62}$")
var RegexpMapKey = regexp.MustCompile("^[a-z0-9][a-z0-9_.\\-]{0,62}$")

func IsValidAttributeName(name string) bool {
	return RegexpPropName.MatchString(name)
}

func IsValidMapKey(key string) bool {
	return RegexpMapKey.MatchString(key)
}

type Model struct {
	Registry   *Registry              `json:"-"`
	Schemas    []string               `json:"schemas,omitempty"`
	Attributes Attributes             `json:"attributes,omitempty"`
	Groups     map[string]*GroupModel `json:"groups,omitempty"` // Plural
}

type Attributes map[string]*Attribute // AttrName->Attr

type Attribute struct {
	Registry       *Registry `json:"-"`
	Name           string    `json:"name,omitempty"`
	Type           string    `json:"type,omitempty"`
	Description    string    `json:"description,omitempty"`
	Enum           []any     `json:"enum,omitempty"` // just scalars though
	Strict         bool      `json:"strict,omitempty"`
	ReadOnly       bool      `json:"readonly,omitempty"`
	ClientRequired bool      `json:"clientrequired,omitempty"`
	ServerRequired bool      `json:"serverrequired,omitempty"`

	Item    *Item               `json:"item,omitempty"`
	IfValue map[string]*IfValue `json:"ifValue,omitempty"` // Value

	// Internal fields
	checkFn  func(e *Entity, newObj map[string]any, oldObj map[string]any) error
	updateFn func(*Entity, bool) error
}

type Item struct {
	Registry   *Registry  `json:"-"`
	Attributes Attributes `json:"attributes,omitempty"` //attrName
	Type       string     `json:"type,omitempty"`
	Item       *Item      `json:"item,omitempty"`
}

type IfValue struct {
	SiblingAttributes Attributes `json:"siblingAttributes,omitempty"`
}

type GroupModel struct {
	SID      string    `json:"-"`
	Registry *Registry `json:"-"`

	Plural     string     `json:"plural"`
	Singular   string     `json:"singular"`
	Attributes Attributes `json:"attributes,omitempty"`

	Resources map[string]*ResourceModel `json:"resources,omitempty"` // Plural
}

type ResourceModel struct {
	SID        string      `json:"-"`
	GroupModel *GroupModel `json:"-"`

	Plural      string     `json:"plural"`
	Singular    string     `json:"singular"`
	Versions    int        `json:"versions"`
	VersionId   bool       `json:"versionid"`
	Latest      bool       `json:"latest"`
	HasDocument bool       `json:"hasdocument"`
	Attributes  Attributes `json:"attributes,omitempty"`
}

func (r *ResourceModel) UnmarshalJSON(data []byte) error {
	// Set the default values
	r.Versions = VERSIONS
	r.VersionId = VERSIONID
	r.Latest = LATEST
	r.HasDocument = HASDOCUMENT

	type tmpResourceModel ResourceModel
	return json.Unmarshal(data, (*tmpResourceModel)(r))
}

func (m *Model) AddSchema(schema string) error {
	err := Do(`INSERT INTO "Schemas" (RegistrySID, "Schema") VALUES(?,?)`,
		m.Registry.DbSID, schema)
	if err != nil {
		err = fmt.Errorf("Error inserting schema(%s): %s", schema, err)
		log.Print(err)
		return err
	}

	for _, s := range m.Schemas {
		if s == schema {
			// already there
			return nil
		}
	}

	m.Schemas = append(m.Schemas, schema)
	return nil
}

func (m *Model) DelSchema(schema string) error {
	err := Do(`DELETE FROM "Schemas" WHERE RegistrySID=? AND "Schema"=?`,
		m.Registry.DbSID, schema)
	if err != nil {
		err = fmt.Errorf("Error deleting schema(%s): %s", schema, err)
		log.Print(err)
		return err
	}

	for i, s := range m.Schemas {
		if s == schema {
			m.Schemas = append(m.Schemas[:i], m.Schemas[i+1:]...)
			return nil
		}
	}
	return nil
}

// Save() should be called by these funcs automatically but there may be
// cases where someone would need to call it manually (e.g. setting an
// attribute's property - we should technically find a way to catch those
// cases so code above this shouldn't need to think about it
func (m *Model) Save() error {
	if err := m.Verify(); err != nil {
		log.Printf("model error: %s", err)
		return err
	}

	buf, _ := json.Marshal(m.Attributes)
	attrs := string(buf)

	err := DoZeroOne(`UPDATE Registries SET Attributes=? WHERE SID=?`,
		attrs, m.Registry.DbSID)
	if err != nil {
		log.Printf("Error updating model: %s", err)
		return err
	}

	for _, gm := range m.Groups {
		if err := gm.Save(); err != nil {
			return err
		}
	}

	return nil
}

func (m *Model) SetSchemas(schemas []string) error {
	err := Do(`DELETE FROM "Schemas" WHERE RegistrySID=?`, m.Registry.DbSID)
	if err != nil {
		err = fmt.Errorf("Error deleting schemas: %s", err)
		log.Print(err)
		return err
	}
	m.Schemas = nil

	for _, s := range schemas {
		err = m.AddSchema(s)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *Model) AddAttr(name, daType string) (*Attribute, error) {
	return m.AddAttribute(&Attribute{Name: name, Type: daType})
}

func (m *Model) AddAttrMap(name string, item *Item) (*Attribute, error) {
	return m.AddAttribute(&Attribute{Name: name, Type: MAP, Item: item})
}

func (m *Model) AddAttrObj(name string) (*Attribute, error) {
	return m.AddAttribute(&Attribute{Name: name, Type: OBJECT, Item: &Item{}})
}

func (m *Model) AddAttrArray(name string, item *Item) (*Attribute, error) {
	return m.AddAttribute(&Attribute{Name: name, Type: ARRAY, Item: item})
}

func (m *Model) AddAttribute(attr *Attribute) (*Attribute, error) {
	if attr == nil {
		return nil, nil
	}

	if attr.Name != "*" && !IsValidAttributeName(attr.Name) {
		return nil, fmt.Errorf("Invalid attribute name: %s", attr.Name)
	}

	if m.Attributes == nil {
		m.Attributes = Attributes{}
	}

	m.Attributes[attr.Name] = attr

	if attr.Registry == nil {
		attr.Registry = m.Registry
	}

	attr.Item.SetRegistry(m.Registry)

	m.Save()

	return attr, nil
}

func (m *Model) DelAttribute(name string) {
	if m.Attributes == nil {
		return
	}

	delete(m.Attributes, name)

	m.Save()
}

func (m *Model) AddGroupModel(plural string, singular string) (*GroupModel, error) {
	if plural == "" {
		return nil, fmt.Errorf("Can't add a GroupModel with an empty plural name")
	}
	if singular == "" {
		return nil, fmt.Errorf("Can't add a GroupModel with an empty sigular name")
	}

	if !IsValidAttributeName(plural) {
		return nil, fmt.Errorf("GroupModel plural name is not valid")
	}

	if !IsValidAttributeName(singular) {
		return nil, fmt.Errorf("GroupModel singular name is not valid")
	}

	for _, gm := range m.Groups {
		if gm.Plural == plural {
			return nil, fmt.Errorf("GroupModel plural %q already exists",
				plural)
		}
		if gm.Singular == singular {
			return nil, fmt.Errorf("GroupModel singular %q already exists",
				singular)
		}
	}

	mSID := NewUUID()
	err := DoOne(`
        INSERT INTO ModelEntities(
            SID, RegistrySID, ParentSID, Plural, Singular, Versions)
        VALUES(?,?,?,?,?,?) `,
		mSID, m.Registry.DbSID, nil, plural, singular, 0)
	if err != nil {
		log.Printf("Error inserting groupModel(%s): %s", plural, err)
		return nil, err
	}
	gm := &GroupModel{
		SID:      mSID,
		Registry: m.Registry,
		Singular: singular,
		Plural:   plural,

		Resources: map[string]*ResourceModel{},
	}

	m.Groups[plural] = gm

	return gm, nil
}

func NewItem(daType string) *Item {
	return &Item{
		Type: daType,
	}
}

func NewItemObject() *Item {
	return &Item{
		Type: OBJECT,
	}
}

func NewItemMap(item *Item) *Item {
	return &Item{
		Type: MAP,
		Item: item,
	}
}

func NewItemArray(item *Item) *Item {
	return &Item{
		Type: ARRAY,
		Item: item,
	}
}

func (i *Item) SetRegistry(reg *Registry) {
	if i == nil {
		return
	}

	i.Registry = reg
	i.Attributes.SetRegistry(reg)
}

func (i *Item) AddAttr(name, daType string) (*Attribute, error) {
	return i.AddAttribute(&Attribute{Name: name, Type: daType})
}

func (i *Item) AddAttrMap(name string, item *Item) (*Attribute, error) {
	return i.AddAttribute(&Attribute{Name: name, Type: MAP, Item: item})
}

func (i *Item) AddAttrObj(name string) (*Attribute, error) {
	return i.AddAttribute(&Attribute{Name: name, Type: OBJECT, Item: &Item{}})
}

func (i *Item) AddAttrArray(name string, item *Item) (*Attribute, error) {
	return i.AddAttribute(&Attribute{Name: name, Type: ARRAY, Item: item})
}

func (i *Item) AddAttribute(attr *Attribute) (*Attribute, error) {
	if attr == nil {
		return nil, nil
	}

	if attr.Name != "*" && !IsValidAttributeName(attr.Name) {
		return nil, fmt.Errorf("Invalid attribute name: %s", attr.Name)
	}

	if i.Attributes == nil {
		i.Attributes = Attributes{}
	}

	i.Attributes[attr.Name] = attr

	if attr.Registry == nil {
		attr.Registry = i.Registry
	}
	if i.Registry != nil {
		i.Registry.Model.Save()
	}

	return attr, nil
}

func (i *Item) DelAttribute(name string) {
	if i.Attributes == nil {
		return
	}

	delete(i.Attributes, name)

	if i.Registry != nil {
		i.Registry.Model.Save()
	}
}

func LoadModel(reg *Registry) *Model {
	groups := map[string]*GroupModel{} // Model SID -> *GroupModel

	model := &Model{
		Registry: reg,
		Groups:   map[string]*GroupModel{},
	}

	// Load Registry Attributes
	results, err := Query(`SELECT Attributes FROM Registries WHERE SID=?`,
		reg.DbSID)
	defer results.Close()
	if err != nil {
		log.Printf("Error loading registries(%s): %s", reg.UID, err)
		return nil
	}
	row := results.NextRow()
	if row == nil {
		log.Printf("Can't find registry: %s", reg.UID)
		return nil
	}

	if row[0] != nil {
		json.Unmarshal([]byte(NotNilString(row[0])), &model.Attributes)
	}

	model.Attributes.SetRegistry(reg)

	// Load Schemas
	results, err = Query(`
        SELECT RegistrySID, "Schema" FROM "Schemas"
        WHERE RegistrySID=?
        ORDER BY "Schema" ASC`, reg.DbSID)
	defer results.Close()

	if err != nil {
		log.Printf("Error loading schemas(%s): %s", reg.UID, err)
		return nil
	}

	for row := results.NextRow(); row != nil; row = results.NextRow() {
		model.Schemas = append(model.Schemas, NotNilString(row[1]))
	}

	// Load Groups & Resources
	results, err = Query(`
        SELECT
            SID, RegistrySID, ParentSID, Plural, Singular, Attributes,
			Versions, VersionId, Latest, HasDocument
        FROM ModelEntities
        WHERE RegistrySID=?
        ORDER BY ParentSID ASC`, reg.DbSID)
	defer results.Close()

	if err != nil {
		log.Printf("Error loading model(%s): %s", reg.UID, err)
		return nil
	}

	for row := results.NextRow(); row != nil; row = results.NextRow() {
		attrs := (Attributes)(nil)
		if row[5] != nil {
			json.Unmarshal([]byte(NotNilString(row[5])), &attrs)
		}

		if *row[2] == nil { // ParentSID nil -> new Group
			g := &GroupModel{ // Plural
				SID:        NotNilString(row[0]), // SID
				Registry:   reg,
				Plural:     NotNilString(row[3]), // Plural
				Singular:   NotNilString(row[4]), // Singular
				Attributes: attrs,

				Resources: map[string]*ResourceModel{},
			}

			model.Groups[NotNilString(row[3])] = g
			groups[NotNilString(row[0])] = g

		} else { // New Resource
			g := groups[NotNilString(row[2])] // Parent SID

			if g != nil { // should always be true, but...
				r := &ResourceModel{
					SID:         NotNilString(row[0]),
					GroupModel:  g,
					Plural:      NotNilString(row[3]),
					Singular:    NotNilString(row[4]),
					Attributes:  attrs,
					Versions:    NotNilIntDef(row[6], VERSIONS),
					VersionId:   NotNilBoolDef(row[7], VERSIONID),
					Latest:      NotNilBoolDef(row[8], LATEST),
					HasDocument: NotNilBoolDef(row[9], HASDOCUMENT),
				}

				g.Resources[r.Plural] = r
			}
		}
	}

	reg.Model = model
	return model
}

func (m *Model) FindGroupModel(gTypePlural string) *GroupModel {
	for _, gModel := range m.Groups {
		if strings.EqualFold(gModel.Plural, gTypePlural) {
			return gModel
		}
	}
	return nil
}

func (m *Model) ApplyNewModel(newM *Model) error {
	// Delete old Schemas, then add new ones
	err := Do(`DELETE FROM "Schemas" WHERE RegistrySID=?`, m.Registry.DbSID)
	if err != nil {
		return err
	}
	for _, schema := range newM.Schemas {
		if err = m.AddSchema(schema); err != nil {
			return err
		}
	}

	// Find all old groups that need to be deleted
	for gmPlural, gm := range m.Groups {
		if newGM, ok := newM.Groups[gmPlural]; !ok {
			if err := gm.Delete(); err != nil {
				return err
			}
		} else {
			for rmPlural, rm := range gm.Resources {
				if _, ok := newGM.Resources[rmPlural]; !ok {
					if err := rm.Delete(); err != nil {
						return err
					}
				}
			}
		}
	}

	// Apply new stuff
	newM.Registry = m.Registry
	for _, newGM := range newM.Groups {
		newGM.Registry = m.Registry
		oldGM := m.Groups[newGM.Plural]
		if oldGM == nil {
			oldGM, err = m.AddGroupModel(newGM.Plural, newGM.Singular)
			if err != nil {
				return err
			}
		} else {
			oldGM.Singular = newGM.Singular
			if err = oldGM.Save(); err != nil {
				return err
			}
		}

		for _, newRM := range newGM.Resources {
			oldRM := oldGM.Resources[newRM.Plural]
			if oldRM == nil {
				oldRM, err = oldGM.AddResourceModel(newRM.Plural,
					newRM.Singular, newRM.Versions, newRM.VersionId,
					newRM.Latest, newRM.HasDocument)
			} else {
				oldRM.Singular = newRM.Singular
				oldRM.Versions = newRM.Versions
				oldRM.VersionId = newRM.VersionId
				oldRM.Latest = newRM.Latest
				oldRM.HasDocument = newRM.HasDocument
				if err = oldRM.Save(); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (gm *GroupModel) Delete() error {
	log.VPrintf(3, ">Enter: Delete.GroupModel: %s", gm.Plural)
	defer log.VPrintf(3, "<Exit: Delete.GroupModel")
	err := DoOne(`
        DELETE FROM ModelEntities
		WHERE RegistrySID=? AND SID=?`, // SID should be enough, but ok
		gm.Registry.DbSID, gm.SID)
	if err != nil {
		log.Printf("Error deleting groupModel(%s): %s", gm.Plural, err)
		return err
	}

	delete(gm.Registry.Model.Groups, gm.Plural)

	return nil
}

func (gm *GroupModel) Save() error {
	// Just updates this GroupModel, not any Resources
	// DO NOT use this to insert a new one

	buf, _ := json.Marshal(gm.Attributes)
	attrs := string(buf)

	err := DoZeroTwo(`
        INSERT INTO ModelEntities(
            SID, RegistrySID,
			ParentSID, Plural, Singular, Attributes)
        VALUES(?,?,?,?,?,?)
        ON DUPLICATE KEY UPDATE
		    ParentSID=?,Plural=?,Singular=?,Attributes=?
		`,
		gm.SID, gm.Registry.DbSID,
		nil, gm.Plural, gm.Singular, attrs,
		nil, gm.Plural, gm.Singular, attrs)
	if err != nil {
		log.Printf("Error updating groupModel(%s): %s", gm.Plural, err)
	}

	for _, rm := range gm.Resources {
		rm.Save()
	}

	return err
}

func (gm *GroupModel) AddAttr(name, daType string) (*Attribute, error) {
	return gm.AddAttribute(&Attribute{Name: name, Type: daType})
}

func (gm *GroupModel) AddAttrMap(name string, item *Item) (*Attribute, error) {
	return gm.AddAttribute(&Attribute{Name: name, Type: MAP, Item: item})
}

func (gm *GroupModel) AddAttrObj(name string) (*Attribute, error) {
	return gm.AddAttribute(&Attribute{Name: name, Type: OBJECT, Item: &Item{}})
}

func (gm *GroupModel) AddAttrArray(name string, item *Item) (*Attribute, error) {
	return gm.AddAttribute(&Attribute{Name: name, Type: ARRAY, Item: item})
}

func (gm *GroupModel) AddAttribute(attr *Attribute) (*Attribute, error) {
	if attr == nil {
		return nil, nil
	}

	if attr.Name != "*" && !IsValidAttributeName(attr.Name) {
		return nil, fmt.Errorf("Invalid attribute name: %s", attr.Name)
	}

	if gm.Attributes == nil {
		gm.Attributes = Attributes{}
	}

	gm.Attributes[attr.Name] = attr

	if attr.Registry == nil {
		attr.Registry = gm.Registry
	}
	gm.Registry.Model.Save()

	return attr, nil
}

func (gm *GroupModel) DelAttribute(name string) {
	if gm.Attributes == nil {
		return
	}

	delete(gm.Attributes, name)

	gm.Registry.Model.Save()
}

func (gm *GroupModel) AddResourceModel(plural string, singular string, versions int, verId bool, latest bool, hasDocument bool) (*ResourceModel, error) {
	if plural == "" {
		return nil, fmt.Errorf("Can't add a group with an empty plural name")
	}
	if singular == "" {
		return nil, fmt.Errorf("Can't add a group with an empty sigular name")
	}
	if versions < 0 {
		return nil, fmt.Errorf("'versions'(%d) must be >= 0", versions)
	}
	if !IsValidAttributeName(plural) {
		return nil, fmt.Errorf("ResourceModel plural name is not valid")
	}
	if !IsValidAttributeName(singular) {
		return nil, fmt.Errorf("ResourceModel singular name is not valid")
	}

	for _, r := range gm.Resources {
		if r.Plural == plural {
			return nil, fmt.Errorf("Resoucre model plural %q already "+
				"exists for group %q", plural, gm.Plural)
		}
		if r.Singular == singular {
			return nil,
				fmt.Errorf("Resoucre model singular %q already "+
					"exists for group %q", singular, gm.Plural)
		}
	}

	mSID := NewUUID()

	err := DoOne(`
		INSERT INTO ModelEntities(
			SID, RegistrySID, ParentSID, Plural, Singular, Versions,
			VersionId, Latest, HasDocument)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		mSID, gm.Registry.DbSID, gm.SID, plural, singular, versions,
		verId, latest, hasDocument)
	if err != nil {
		log.Printf("Error inserting resourceModel(%s): %s", plural, err)
		return nil, err
	}
	r := &ResourceModel{
		SID:         mSID,
		GroupModel:  gm,
		Singular:    singular,
		Plural:      plural,
		Versions:    versions,
		VersionId:   verId,
		Latest:      latest,
		HasDocument: hasDocument,
	}

	gm.Resources[plural] = r

	return r, nil
}

func (rm *ResourceModel) Delete() error {
	log.VPrintf(3, ">Enter: Delete.ResourceModel: %s", rm.Plural)
	defer log.VPrintf(3, "<Exit: Delete.ResourceModel")
	err := DoOne(`
        DELETE FROM ModelEntities
		WHERE RegistrySID=? AND SID=?`, // SID should be enough, but ok
		rm.GroupModel.Registry.DbSID, rm.SID)
	if err != nil {
		log.Printf("Error deleting resourceModel(%s): %s", rm.Plural, err)
		return err
	}

	delete(rm.GroupModel.Resources, rm.Plural)

	return nil
}

func (rm *ResourceModel) Save() error {
	// Just updates this GroupModel, not any Resources
	// DO NOT use this to insert a new one

	buf, _ := json.Marshal(rm.Attributes)
	attrs := string(buf)

	err := DoZeroTwo(`
        INSERT INTO ModelEntities(
            SID, RegistrySID,
			ParentSID, Plural, Singular, Versions,
			Attributes,
			VersionId, Latest, HasDocument)
        VALUES(?,?,?,?,?,?,?,?,?,?)
        ON DUPLICATE KEY UPDATE
            ParentSID=?, Plural=?, Singular=?,
			Attributes=?,
            Versions=?, VersionId=?, Latest=?, HasDocument=?`,
		rm.SID, rm.GroupModel.Registry.DbSID,
		rm.GroupModel.SID, rm.Plural, rm.Singular, rm.Versions,
		attrs,
		rm.VersionId, rm.Latest, rm.HasDocument,

		rm.GroupModel.SID, rm.Plural, rm.Singular,
		attrs,
		rm.Versions, rm.VersionId, rm.Latest, rm.HasDocument)
	if err != nil {
		log.Printf("Error updating resourceModel(%s): %s", rm.Plural, err)
		return err
	}
	return err
}

func (rm *ResourceModel) AddAttr(name, daType string) (*Attribute, error) {
	return rm.AddAttribute(&Attribute{Name: name, Type: daType})
}

func (rm *ResourceModel) AddAttrMap(name string, item *Item) (*Attribute, error) {
	return rm.AddAttribute(&Attribute{Name: name, Type: MAP, Item: item})
}

func (rm *ResourceModel) AddAttrObj(name string) (*Attribute, error) {
	return rm.AddAttribute(&Attribute{Name: name, Type: OBJECT, Item: &Item{}})
}

func (rm *ResourceModel) AddAttrArray(name string, item *Item) (*Attribute, error) {
	return rm.AddAttribute(&Attribute{Name: name, Type: ARRAY, Item: item})
}

func (rm *ResourceModel) AddAttribute(attr *Attribute) (*Attribute, error) {
	if attr == nil {
		return nil, nil
	}

	if attr.Name != "*" && !IsValidAttributeName(attr.Name) {
		return nil, fmt.Errorf("Invalid attribute name: %s", attr.Name)
	}

	if rm.Attributes == nil {
		rm.Attributes = Attributes{}
	}

	rm.Attributes[attr.Name] = attr

	if attr.Registry == nil {
		attr.Registry = rm.GroupModel.Registry
	}
	rm.GroupModel.Registry.Model.Save()

	return attr, nil
}

func (rm *ResourceModel) DelAttribute(name string) {
	if rm.Attributes == nil {
		return
	}

	delete(rm.Attributes, name)

	rm.GroupModel.Registry.Model.Save()
}

func (attrs *Attributes) SetRegistry(reg *Registry) {
	if attrs == nil {
		return
	}

	for _, attr := range *attrs {
		attr.Registry = reg
		attr.Item.SetRegistry(reg)
	}
}

func (attrs Attributes) AddIfValueAttributes(obj map[string]any) {
	attrNames := Keys(attrs)
	for i := 0; i < len(attrNames); i++ { // since attrs changes
		attr := attrs[attrNames[i]]
		if len(attr.IfValue) == 0 || attr.Name == "*" {
			continue
		}

		val, ok := obj[attr.Name]
		if !ok {
			continue
		}

		valStr := fmt.Sprintf("%v", val)
		for ifValStr, ifValueData := range attr.IfValue {
			if ifValStr != valStr {
				continue
			}

			for _, newAttr := range ifValueData.SiblingAttributes {
				if _, ok := attrs[newAttr.Name]; ok {
					Panicf(`Attribute %q has an ifvalue(%s) that `+
						`conflicts with an existing attribute`,
						attr.Name, newAttr.Name)
				}
				attrs[newAttr.Name] = newAttr
				attrNames = append(attrNames, newAttr.Name)
			}
		}
	}
}

func KindIsScalar(k reflect.Kind) bool {
	// SOOOO risky :-)
	return k < reflect.Array || k == reflect.String
}

func IsScalar(daType string) bool {
	return daType == BOOLEAN || daType == DECIMAL || daType == INTEGER ||
		daType == STRING || daType == TIMESTAMP || daType == UINTEGER ||
		daType == URI || daType == URI_REFERENCE || daType == URI_TEMPLATE ||
		daType == URL
}

// Is some string variant
func IsString(daType string) bool {
	return daType == STRING || daType == TIMESTAMP ||
		daType == URI || daType == URI_REFERENCE || daType == URI_TEMPLATE ||
		daType == URL
}

func (a *Attribute) IsScalar() bool {
	return IsScalar(a.Type)
}

func (a *Attribute) AddAttr(name, daType string) (*Attribute, error) {
	return a.AddAttribute(&Attribute{
		Registry: a.Registry,
		Name:     name,
		Type:     daType,
	})
}

func (a *Attribute) AddAttrMap(name string, item *Item) (*Attribute, error) {
	return a.AddAttribute(&Attribute{Name: name, Type: MAP, Item: item})
}

func (a *Attribute) AddAttrObj(name string) (*Attribute, error) {
	return a.AddAttribute(&Attribute{Name: name, Type: OBJECT, Item: &Item{}})
}

func (a *Attribute) AddAttrArray(name string, item *Item) (*Attribute, error) {
	return a.AddAttribute(&Attribute{Name: name, Type: ARRAY, Item: item})
}
func (a *Attribute) AddAttribute(attr *Attribute) (*Attribute, error) {
	if attr.Name != "*" && !IsValidAttributeName(attr.Name) {
		return nil, fmt.Errorf("Invalid attribute name: %s", attr.Name)
	}

	if a.Item.Attributes == nil {
		a.Item.Attributes = Attributes{}
	}

	a.Item.Attributes[attr.Name] = attr
	attr.Registry = a.Registry
	a.Registry.Model.Save()
	return attr, nil
}

// Note this will also validate that the names used to build up the path
// of the attribute are valid (e.g. wrt case and valid chars)
func GetAttributeType(e *Entity, rSID string, obj map[string]any, abstract string, pp *PropPath) (string, error) {
	log.VPrintf(3, ">Enter: GetAttributeType: %s / %s", abstract, pp.UI())
	defer log.VPrintf(3, "<Exit: GetAttributeType")

	if pp.Len() == 0 {
		panic("PropPath can't be empty for GetAttributeType")
	}

	if pp.Top()[0] == '#' {
		return ANY, nil
	}

	savePP := pp.Clone()

	attrs := e.GetAttributes(true)
	if attrs == nil {
		panic("Attributes can't be nil for: %s" + abstract)
	}

	item := &Item{ // model definition of current thing we're processing
		Attributes: attrs,
		Type:       OBJECT,
		Item:       nil,
	}
	prevTop := ""
	top := ""

	for {
		log.VPrintf(3, "PP: %q Item: %#q", pp.UI(), item)
		// Nothing to do so just return current item
		if pp.Len() == 0 {
			return item.Type, nil
		}

		prevTop = top

		top = pp.Top()

		if IsScalar(item.Type) {
			if pp.Len() != 0 {
				sub := ""
				if pp.Parts[0].Index >= 0 {
					sub = fmt.Sprintf("[%d]", pp.Parts[0].Index)
				}
				return "", fmt.Errorf("Traversing into scalar "+
					"\"%s%s\": %s", prevTop, sub, savePP.UI())
			}
			return item.Type, nil
		}

		if item.Type == ANY {
			return ANY, nil
		}

		if item.Type == OBJECT {
			// We're looking at the attribute name in the object
			if !IsValidAttributeName(top) {
				return "", fmt.Errorf("Attribute name %q isn't valid", top)
			}

			attr := item.Attributes[top]
			if attr == nil {
				attr = item.Attributes["*"]
				if attr == nil {
					return "", nil
				}
			}

			if attr.Type == ANY {
				return attr.Type, nil
			}

			item = &Item{
				Attributes: nil,
				Type:       attr.Type,
				Item:       attr.Item,
			}

			if attr.Item != nil {
				item.Attributes = attr.Item.Attributes
			}
		} else if item.Type == ARRAY {
			// We're looking at the index of the array
			if pp.Parts[0].Index < 0 {
				return "", fmt.Errorf("Array index %q isn't an integer", top)
			}

			item = item.Item
		} else if item.Type == MAP {
			// We're looking at the map key name
			if !IsValidMapKey(top) {
				return "", fmt.Errorf("Map key %q isn't valid", top)
			}
			item = item.Item
		} else {
			panic(fmt.Sprintf("Can't deal with type: %s\npp:%v",
				item.Type, savePP.UI()))
		}

		pp = pp.Next()
	}
}

func (m *Model) Verify() error {
	// Check Registry attributes
	for name, attr := range m.Attributes {
		if err := attr.Verify(name); err != nil {
			return err
		}
	}

	for gmName, gm := range m.Groups {
		if err := gm.Verify(gmName); err != nil {
			return err
		}
	}

	return nil
}

func (gm *GroupModel) Verify(gmName string) error {
	if !IsValidAttributeName(gmName) {
		return fmt.Errorf("Invalid Group name/key %q - must match %q",
			gmName, RegexpPropName.String())
	}

	if gm.Plural != gmName {
		return fmt.Errorf("Group %q must have a `plural` value of %q, not %q",
			gmName, gmName, gm.Plural)
	}

	if !IsValidAttributeName(gm.Singular) {
		return fmt.Errorf("Invalid Group 'singular' value %q - must match %q",
			gm.Singular, RegexpPropName.String())
	}

	for attrName, attr := range gm.Attributes {
		if err := attr.Verify(attrName); err != nil {
			return err
		}
	}

	for rmName, rm := range gm.Resources {
		if err := rm.Verify(rmName); err != nil {
			return err
		}
	}

	return nil
}

func (rm *ResourceModel) Verify(rmName string) error {
	if !IsValidAttributeName(rmName) {
		return fmt.Errorf("Invalid Resource name/key %q - must match %q",
			rmName, RegexpPropName.String())
	}

	if rm.Plural != rmName {
		return fmt.Errorf("Resource %q must have a 'plural' value of %q, "+
			"not %q", rmName, rmName, rm.Plural)
	}

	if rm.Versions < 0 {
		return fmt.Errorf("Resource %q must have a 'versions' value >= 0",
			rmName)
	}

	for attrName, attr := range rm.Attributes {
		if err := attr.Verify(attrName); err != nil {
			return err
		}
	}

	return nil
}

var DefinedTypes = map[string]bool{
	ANY:     true,
	BOOLEAN: true,
	DECIMAL: true, INTEGER: true, UINTEGER: true,
	ARRAY:     true,
	MAP:       true,
	OBJECT:    true,
	STRING:    true,
	TIMESTAMP: true,
	URI:       true, URI_REFERENCE: true, URI_TEMPLATE: true, URL: true}

func (attr *Attribute) Verify(name string) error {
	if name != "*" && !IsValidAttributeName(name) {
		return fmt.Errorf("Invalid Attribute name/key '%s' - must match '%s'",
			name, RegexpPropName.String())
	}

	if name != attr.Name {
		return fmt.Errorf("Attribute '%s's 'name' property must be '%s' not "+
			"'%s'", name, name, attr.Name)
	}

	if _, ok := DefinedTypes[attr.Type]; !ok {
		return fmt.Errorf("Attribute '%s' has an invalid type: '%s'",
			name, attr.Type)
	}

	// Is it ok for strict=true and enum=[] ? Require no value???
	// if attr.Strict == true && len(attr.Enum) == 0 {
	// }

	// check enum values
	if attr.Enum != nil && !IsScalar(attr.Type) {
		return fmt.Errorf("Attribute '%s' must be a scalar, not: %s",
			attr.Name, attr.Type)
	}
	if len(attr.Enum) > 0 {
		for _, val := range attr.Enum {
			if !IsOfType(val, attr.Type) {
				return fmt.Errorf("Attribute '%s' enum value of '%v' must be "+
					"of type: %s", attr.Name, val, attr.Type)
			}
		}
	}

	if attr.ClientRequired && !attr.ServerRequired {
		return fmt.Errorf("Attribute '%s' is 'clientrequired' so "+
			"'serverrequired' must be 'true' as well", attr.Name)
	}

	/* Not true since Item defaults to empty Object - odd but ok
	if !IsScalar(attr.Type) && attr.Type != OBJECT && attr.Item == nil {
		return fmt.Errorf("Attribute '%s' is non-scalar so it must have an "+
			"item section", attr.Name)
	}
	*/

	if attr.Item != nil {
		if IsScalar(attr.Type) || attr.Type == "ANY" {
			return fmt.Errorf("Attribute '%s' can't have an 'item' section "+
				"because its of type '%s'", name, attr.Type)
		}
		if err := attr.Item.Verify(attr.Name, attr.Type); err != nil {
			return err
		}
	}

	// check ifvalues

	return nil
}

func (i *Item) Verify(attrName string, attrType string) error {
	iType := i.Type
	if iType == "" {
		iType = OBJECT
	}

	if _, ok := DefinedTypes[iType]; !ok {
		return fmt.Errorf("Attribute '%s' has an invalid item.type: %s",
			attrName, iType)
	}

	if attrType == OBJECT && iType != OBJECT {
		return fmt.Errorf("Attribute '%s' must not have an item.type of: "+
			"object", attrName)
	}

	if _, ok := DefinedTypes[iType]; !ok {
		return fmt.Errorf("Attribute '%s's 'item.type' isn't a valid "+
			"type: '%s'", attrName, iType)

	}

	if i.Item != nil {
		if IsScalar(iType) || iType == "ANY" {
			return fmt.Errorf("Attribute '%s' can't have an 'item' section "+
				"because its parent type is '%s'",
				attrName, iType)
		}
		if err := i.Item.Verify(attrName, iType); err != nil {
			return err
		}
	}

	return nil
}

// attr.Type must be a scalar
// Used to check JSON type vs our types
func IsOfType(val any, attrType string) bool {
	switch reflect.ValueOf(val).Kind() {
	case reflect.Bool:
		return attrType == BOOLEAN

	case reflect.String:
		if attrType == TIMESTAMP {
			str := val.(string)
			_, err := time.Parse(time.RFC3339, str)
			return err == nil
		}

		return IsString(attrType)

	case reflect.Float64: // JSON ints show up as floats
		if attrType == DECIMAL {
			return true
		}
		if attrType == INTEGER || attrType == UINTEGER {
			valInt := int(val.(float64))
			if float64(valInt) != val.(float64) {
				return false
			}
			return attrType == INTEGER || valInt >= 0
		}
		return false

	case reflect.Int:
		if attrType == DECIMAL {
			return true
		}
		if attrType == INTEGER || attrType == UINTEGER {
			valInt := val.(int)
			return attrType == INTEGER || valInt >= 0
		}
		return false

	default:
		return false
	}
}

/*
func AbstractToSingular(reg *Registry, abs string) string {
	absParts := strings.Split(abs, string(DB_IN))

	if len(absParts) == 0 {
		panic("help")
	}
	gm := reg.Model.Groups[absParts[0]]
	PanicIf(gm == nil, "no gm")

	if len(absParts) == 1 {
		return gm.Singular
	}

	rm := gm.Resources[absParts[1]]
	PanicIf(rm == nil, "no rm")
	return rm.Singular
}
*/
