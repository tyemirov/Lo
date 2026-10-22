use serde::de::{self, Deserialize, Deserializer, MapAccess, SeqAccess, Visitor};
use serde::ser::{Serialize, SerializeMap, Serializer};
use serde_json::Value as Json;
use std::{fmt, ops::Index};

const KEYS: [&str; 15] = [
    "action",
    "arguments",
    "derivative_frame",
    "derivative_node",
    "derivative_plan",
    "field",
    "file",
    "float_bits",
    "flow",
    "id",
    "labels",
    "operation",
    "owner",
    "text",
    "type",
];
fn slot(key: &str) -> Option<usize> {
    match key {
        "action" => Some(0),
        "arguments" => Some(1),
        "derivative_frame" => Some(2),
        "derivative_node" => Some(3),
        "derivative_plan" => Some(4),
        "field" => Some(5),
        "file" => Some(6),
        "float_bits" => Some(7),
        "flow" => Some(8),
        "id" => Some(9),
        "labels" => Some(10),
        "operation" => Some(11),
        "owner" => Some(12),
        "text" => Some(13),
        "type" => Some(14),
        _ => None,
    }
}
#[derive(Clone)]
pub struct Location(Option<Box<LocationValues>>);
#[derive(Clone)]
struct LocationValues {
    fields: [Json; KEYS.len()],
    coordinates: [Coordinate; 2],
}
impl Default for LocationValues {
    fn default() -> Self {
        Self {
            fields: std::array::from_fn(|_| Json::Null),
            coordinates: std::array::from_fn(|_| Coordinate::default()),
        }
    }
}
impl Location {
    pub fn coordinate(&self, key: &str) -> &Coordinate {
        static NULL: Coordinate = Coordinate::Other(Json::Null);
        let index = match key {
            "start" => 0,
            "end" => 1,
            _ => return &NULL,
        };
        self.0
            .as_ref()
            .map_or(&NULL, |values| &values.coordinates[index])
    }
}
impl Default for Location {
    fn default() -> Self {
        Self(None)
    }
}
impl Index<&str> for Location {
    type Output = Json;
    fn index(&self, key: &str) -> &Json {
        static NULL: Json = Json::Null;
        self.0
            .as_ref()
            .and_then(|values| slot(key).map(|index| &values.fields[index]))
            .unwrap_or(&NULL)
    }
}
pub trait Fields {
    fn field(&self, key: &str) -> &Json;
}
impl Fields for Json {
    fn field(&self, key: &str) -> &Json {
        &self[key]
    }
}
impl Fields for Location {
    fn field(&self, key: &str) -> &Json {
        &self[key]
    }
}
enum Key {
    Field(usize),
    Coordinate(usize),
    Unknown,
}
impl Key {
    fn from_name(key: &str) -> Self {
        match key {
            "start" => Self::Coordinate(0),
            "end" => Self::Coordinate(1),
            _ => slot(key).map_or(Self::Unknown, Self::Field),
        }
    }
}
impl<'de> Deserialize<'de> for Key {
    fn deserialize<D: Deserializer<'de>>(d: D) -> Result<Self, D::Error> {
        struct KeyVisitor;
        impl<'de> Visitor<'de> for KeyVisitor {
            type Value = Key;
            fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
                f.write_str("an object key")
            }
            fn visit_str<E: de::Error>(self, value: &str) -> Result<Key, E> {
                Ok(Key::from_name(value))
            }
            fn visit_string<E: de::Error>(self, value: String) -> Result<Key, E> {
                self.visit_str(&value)
            }
        }
        d.deserialize_str(KeyVisitor)
    }
}
// Non-object/array shapes still parse their entire JSON value. Validation remains in Program::parse.
macro_rules! scalar_visits {
    ($value:expr) => {
        fn visit_unit<E: de::Error>(self) -> Result<Self::Value, E> {
            Ok($value)
        }
        fn visit_bool<E: de::Error>(self, _: bool) -> Result<Self::Value, E> {
            Ok($value)
        }
        fn visit_i64<E: de::Error>(self, _: i64) -> Result<Self::Value, E> {
            Ok($value)
        }
        fn visit_u64<E: de::Error>(self, _: u64) -> Result<Self::Value, E> {
            Ok($value)
        }
        fn visit_f64<E: de::Error>(self, _: f64) -> Result<Self::Value, E> {
            Ok($value)
        }
        fn visit_str<E: de::Error>(self, _: &str) -> Result<Self::Value, E> {
            Ok($value)
        }
        fn visit_string<E: de::Error>(self, _: String) -> Result<Self::Value, E> {
            Ok($value)
        }
    };
}
#[derive(Clone)]
pub enum Coordinate {
    Other(Json),
    Object(Option<Box<CoordinateValues>>),
}
#[derive(Clone)]
pub struct CoordinateValues {
    values: [Json; 3],
    present: u8,
    extras: Option<serde_json::Map<String, Json>>,
}
impl Default for CoordinateValues {
    fn default() -> Self {
        Self {
            values: std::array::from_fn(|_| Json::Null),
            present: 0,
            extras: None,
        }
    }
}
impl Default for Coordinate {
    fn default() -> Self {
        Self::Other(Json::Null)
    }
}
fn coordinate_slot(key: &str) -> Option<usize> {
    match key {
        "column" => Some(0),
        "line" => Some(1),
        "offset" => Some(2),
        _ => None,
    }
}
impl Fields for Coordinate {
    fn field(&self, key: &str) -> &Json {
        static NULL: Json = Json::Null;
        match self {
            Self::Other(value) => &value[key],
            Self::Object(None) => &NULL,
            Self::Object(Some(object)) => coordinate_slot(key)
                .map(|index| &object.values[index])
                .or_else(|| object.extras.as_ref().and_then(|extras| extras.get(key)))
                .unwrap_or(&NULL),
        }
    }
}
impl Serialize for Coordinate {
    fn serialize<S: Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        match self {
            Self::Other(value) => value.serialize(serializer),
            Self::Object(None) => serializer.serialize_map(Some(0))?.end(),
            Self::Object(Some(object)) => {
                let count = object.present.count_ones() as usize
                    + object.extras.as_ref().map_or(0, |extras| extras.len());
                let mut map = serializer.serialize_map(Some(count))?;
                let mut extras = object
                    .extras
                    .as_ref()
                    .into_iter()
                    .flat_map(|extras| extras.iter())
                    .peekable();
                for (index, key) in ["column", "line", "offset"].into_iter().enumerate() {
                    while extras.peek().is_some_and(|(extra, _)| extra.as_str() < key) {
                        let (extra, value) = extras.next().unwrap();
                        map.serialize_entry(extra, value)?;
                    }
                    if object.present & (1 << index) != 0 {
                        map.serialize_entry(key, &object.values[index])?;
                    }
                }
                for (extra, value) in extras {
                    map.serialize_entry(extra, value)?;
                }
                map.end()
            }
        }
    }
}
enum CoordinateKey {
    Known(usize),
    Extra(String),
}
impl<'de> Deserialize<'de> for CoordinateKey {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        struct KeyVisitor;
        impl<'de> Visitor<'de> for KeyVisitor {
            type Value = CoordinateKey;
            fn expecting(&self, formatter: &mut fmt::Formatter) -> fmt::Result {
                formatter.write_str("an object key")
            }
            fn visit_str<E: de::Error>(self, value: &str) -> Result<Self::Value, E> {
                Ok(match coordinate_slot(value) {
                    Some(index) => CoordinateKey::Known(index),
                    None => CoordinateKey::Extra(value.into()),
                })
            }
            fn visit_string<E: de::Error>(self, value: String) -> Result<Self::Value, E> {
                Ok(match coordinate_slot(&value) {
                    Some(index) => CoordinateKey::Known(index),
                    None => CoordinateKey::Extra(value),
                })
            }
        }
        deserializer.deserialize_str(KeyVisitor)
    }
}
impl<'de> Deserialize<'de> for Coordinate {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        struct CoordinateVisitor;
        impl<'de> Visitor<'de> for CoordinateVisitor {
            type Value = Coordinate;
            fn expecting(&self, formatter: &mut fmt::Formatter) -> fmt::Result {
                formatter.write_str("a JSON value")
            }
            fn visit_unit<E: de::Error>(self) -> Result<Self::Value, E> {
                Ok(Coordinate::default())
            }
            fn visit_bool<E: de::Error>(self, value: bool) -> Result<Self::Value, E> {
                Ok(Coordinate::Other(Json::Bool(value)))
            }
            fn visit_i64<E: de::Error>(self, value: i64) -> Result<Self::Value, E> {
                Ok(Coordinate::Other(Json::from(value)))
            }
            fn visit_u64<E: de::Error>(self, value: u64) -> Result<Self::Value, E> {
                Ok(Coordinate::Other(Json::from(value)))
            }
            fn visit_f64<E: de::Error>(self, value: f64) -> Result<Self::Value, E> {
                Ok(Coordinate::Other(Json::from(value)))
            }
            fn visit_str<E: de::Error>(self, value: &str) -> Result<Self::Value, E> {
                Ok(Coordinate::Other(Json::String(value.into())))
            }
            fn visit_string<E: de::Error>(self, value: String) -> Result<Self::Value, E> {
                Ok(Coordinate::Other(Json::String(value)))
            }
            fn visit_seq<A: SeqAccess<'de>>(self, sequence: A) -> Result<Self::Value, A::Error> {
                Json::deserialize(de::value::SeqAccessDeserializer::new(sequence))
                    .map(Coordinate::Other)
            }
            fn visit_map<A: MapAccess<'de>>(self, mut map: A) -> Result<Self::Value, A::Error> {
                let mut object: Option<Box<CoordinateValues>> = None;
                while let Some(key) = map.next_key::<CoordinateKey>()? {
                    let value = map.next_value::<Json>()?;
                    let object =
                        object.get_or_insert_with(|| Box::new(CoordinateValues::default()));
                    match key {
                        CoordinateKey::Known(index) => {
                            object.values[index] = value;
                            object.present |= 1 << index;
                        }
                        CoordinateKey::Extra(key) => {
                            object
                                .extras
                                .get_or_insert_with(serde_json::Map::new)
                                .insert(key, value);
                        }
                    }
                }
                Ok(Coordinate::Object(object))
            }
        }
        deserializer.deserialize_any(CoordinateVisitor)
    }
}

impl<'de> Deserialize<'de> for Location {
    fn deserialize<D: Deserializer<'de>>(d: D) -> Result<Self, D::Error> {
        struct LocationVisitor;
        impl<'de> Visitor<'de> for LocationVisitor {
            type Value = Location;
            fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
                f.write_str("a JSON value")
            }
            scalar_visits!(Location::default());
            fn visit_seq<A: SeqAccess<'de>>(self, seq: A) -> Result<Location, A::Error> {
                Json::deserialize(de::value::SeqAccessDeserializer::new(seq))?;
                Ok(Location::default())
            }
            fn visit_map<A: MapAccess<'de>>(self, mut map: A) -> Result<Location, A::Error> {
                let mut location = Location::default();
                while let Some(key) = map.next_key::<Key>()? {
                    match key {
                        Key::Field(index) => {
                            let value = map.next_value::<Json>()?;
                            location
                                .0
                                .get_or_insert_with(|| Box::new(LocationValues::default()))
                                .fields[index] = value;
                        }
                        Key::Coordinate(index) => {
                            let value = map.next_value::<Coordinate>()?;
                            location
                                .0
                                .get_or_insert_with(|| Box::new(LocationValues::default()))
                                .coordinates[index] = value;
                        }
                        Key::Unknown => {
                            map.next_value::<Json>()?;
                        }
                    }
                }
                Ok(location)
            }
        }
        d.deserialize_any(LocationVisitor)
    }
}
struct Locations(Option<Vec<Location>>);
impl<'de> Deserialize<'de> for Locations {
    fn deserialize<D: Deserializer<'de>>(d: D) -> Result<Self, D::Error> {
        struct LocationsVisitor;
        impl<'de> Visitor<'de> for LocationsVisitor {
            type Value = Locations;
            fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
                f.write_str("a JSON value")
            }
            scalar_visits!(Locations(None));
            fn visit_seq<A: SeqAccess<'de>>(self, mut seq: A) -> Result<Locations, A::Error> {
                let mut locations = Vec::new();
                while let Some(location) = seq.next_element()? {
                    locations.push(location);
                }
                Ok(Locations(Some(locations)))
            }
            fn visit_map<A: MapAccess<'de>>(self, map: A) -> Result<Locations, A::Error> {
                Json::deserialize(de::value::MapAccessDeserializer::new(map))?;
                Ok(Locations(None))
            }
        }
        d.deserialize_any(LocationsVisitor)
    }
}
pub struct Document {
    pub root: Json,
    pub locations: Option<Vec<Location>>,
}
impl Default for Document {
    fn default() -> Self {
        Self {
            root: Json::Null,
            locations: None,
        }
    }
}
impl<'de> Deserialize<'de> for Document {
    fn deserialize<D: Deserializer<'de>>(d: D) -> Result<Self, D::Error> {
        struct DocumentVisitor;
        impl<'de> Visitor<'de> for DocumentVisitor {
            type Value = Document;
            fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
                f.write_str("a JSON value")
            }
            scalar_visits!(Document::default());
            fn visit_seq<A: SeqAccess<'de>>(self, seq: A) -> Result<Document, A::Error> {
                Json::deserialize(de::value::SeqAccessDeserializer::new(seq))?;
                Ok(Document::default())
            }
            fn visit_map<A: MapAccess<'de>>(self, mut map: A) -> Result<Document, A::Error> {
                let mut root = serde_json::Map::new();
                let mut locations = None;
                while let Some(key) = map.next_key::<String>()? {
                    if key == "locations" {
                        locations = map.next_value::<Locations>()?.0;
                    } else {
                        root.insert(key, map.next_value::<Json>()?);
                    }
                }
                Ok(Document {
                    root: Json::Object(root),
                    locations,
                })
            }
        }
        d.deserialize_any(DocumentVisitor)
    }
}
