use serde::de::{self, Deserialize, Deserializer, MapAccess, SeqAccess, Visitor};
use serde::ser::{Serialize, SerializeMap, Serializer};
use serde_json::Value as Json;
use std::{collections::BTreeSet, fmt, sync::Arc};

pub trait Fields {
    fn field(&self, key: &str) -> &Json;
}
impl Fields for Json {
    fn field(&self, key: &str) -> &Json {
        &self[key]
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

#[derive(Clone)]
pub struct SourceSpan {
    pub file: Arc<str>,
    pub start: Coordinate,
    pub end: Coordinate,
}
impl SourceSpan {
    pub fn source_record(&self) -> Json {
        serde_json::json!({"file":self.file.as_ref(),"start":self.start,"end":self.end})
    }
}
#[derive(Default)]
pub struct SpanRecord {
    pub file_index: Json,
    pub start: Coordinate,
    pub end: Coordinate,
    pub unknown_fields: BTreeSet<String>,
}
impl<'de> Deserialize<'de> for SpanRecord {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        struct SpanVisitor;
        impl<'de> Visitor<'de> for SpanVisitor {
            type Value = SpanRecord;
            fn expecting(&self, formatter: &mut fmt::Formatter) -> fmt::Result {
                formatter.write_str("a JSON value")
            }
            scalar_visits!(SpanRecord::default());
            fn visit_seq<A: SeqAccess<'de>>(self, sequence: A) -> Result<Self::Value, A::Error> {
                Json::deserialize(de::value::SeqAccessDeserializer::new(sequence))?;
                Ok(SpanRecord::default())
            }
            fn visit_map<A: MapAccess<'de>>(self, mut map: A) -> Result<Self::Value, A::Error> {
                let mut span = SpanRecord::default();
                while let Some(key) = map.next_key::<String>()? {
                    match key.as_str() {
                        "file_index" => span.file_index = map.next_value()?,
                        "start" => span.start = map.next_value()?,
                        "end" => span.end = map.next_value()?,
                        _ => {
                            map.next_value::<Json>()?;
                            span.unknown_fields.insert(key);
                        }
                    }
                }
                Ok(span)
            }
        }
        deserializer.deserialize_any(SpanVisitor)
    }
}
struct Spans(Option<Vec<SpanRecord>>);
impl<'de> Deserialize<'de> for Spans {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        struct SpansVisitor;
        impl<'de> Visitor<'de> for SpansVisitor {
            type Value = Spans;
            fn expecting(&self, formatter: &mut fmt::Formatter) -> fmt::Result {
                formatter.write_str("a JSON value")
            }
            scalar_visits!(Spans(None));
            fn visit_seq<A: SeqAccess<'de>>(
                self,
                mut sequence: A,
            ) -> Result<Self::Value, A::Error> {
                let mut spans = Vec::new();
                while let Some(span) = sequence.next_element()? {
                    spans.push(span);
                }
                Ok(Spans(Some(spans)))
            }
            fn visit_map<A: MapAccess<'de>>(self, map: A) -> Result<Self::Value, A::Error> {
                Json::deserialize(de::value::MapAccessDeserializer::new(map))?;
                Ok(Spans(None))
            }
        }
        deserializer.deserialize_any(SpansVisitor)
    }
}
fn row_slot(key: &str) -> Option<usize> {
    match key {
        "span" => Some(0),
        "action" => Some(1),
        "type" => Some(2),
        "owner" => Some(3),
        "flow" => Some(4),
        "payload" => Some(5),
        _ => None,
    }
}
pub struct RowRecord {
    pub object: bool,
    values: [Json; 6],
    pub unknown_fields: BTreeSet<String>,
}
impl Default for RowRecord {
    fn default() -> Self {
        Self {
            object: false,
            values: std::array::from_fn(|_| Json::Null),
            unknown_fields: BTreeSet::new(),
        }
    }
}
impl Fields for RowRecord {
    fn field(&self, key: &str) -> &Json {
        static NULL: Json = Json::Null;
        row_slot(key)
            .map(|index| &self.values[index])
            .unwrap_or(&NULL)
    }
}
enum RowKey {
    Known(usize),
    Extra(String),
}
impl<'de> Deserialize<'de> for RowKey {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        struct KeyVisitor;
        impl<'de> Visitor<'de> for KeyVisitor {
            type Value = RowKey;
            fn expecting(&self, formatter: &mut fmt::Formatter) -> fmt::Result {
                formatter.write_str("an object key")
            }
            fn visit_str<E: de::Error>(self, value: &str) -> Result<Self::Value, E> {
                Ok(match row_slot(value) {
                    Some(index) => RowKey::Known(index),
                    None => RowKey::Extra(value.into()),
                })
            }
            fn visit_string<E: de::Error>(self, value: String) -> Result<Self::Value, E> {
                Ok(match row_slot(&value) {
                    Some(index) => RowKey::Known(index),
                    None => RowKey::Extra(value),
                })
            }
        }
        deserializer.deserialize_str(KeyVisitor)
    }
}
impl<'de> Deserialize<'de> for RowRecord {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        struct RowVisitor;
        impl<'de> Visitor<'de> for RowVisitor {
            type Value = RowRecord;
            fn expecting(&self, formatter: &mut fmt::Formatter) -> fmt::Result {
                formatter.write_str("a JSON value")
            }
            scalar_visits!(RowRecord::default());
            fn visit_seq<A: SeqAccess<'de>>(self, sequence: A) -> Result<Self::Value, A::Error> {
                Json::deserialize(de::value::SeqAccessDeserializer::new(sequence))?;
                Ok(RowRecord::default())
            }
            fn visit_map<A: MapAccess<'de>>(self, mut map: A) -> Result<Self::Value, A::Error> {
                let mut row = RowRecord {
                    object: true,
                    ..RowRecord::default()
                };
                while let Some(key) = map.next_key::<RowKey>()? {
                    let value = map.next_value::<Json>()?;
                    match key {
                        RowKey::Known(index) => row.values[index] = value,
                        RowKey::Extra(key) => {
                            row.unknown_fields.insert(key);
                        }
                    }
                }
                Ok(row)
            }
        }
        deserializer.deserialize_any(RowVisitor)
    }
}
struct Rows(Option<Vec<RowRecord>>);
impl<'de> Deserialize<'de> for Rows {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        struct RowsVisitor;
        impl<'de> Visitor<'de> for RowsVisitor {
            type Value = Rows;
            fn expecting(&self, formatter: &mut fmt::Formatter) -> fmt::Result {
                formatter.write_str("a JSON value")
            }
            scalar_visits!(Rows(None));
            fn visit_seq<A: SeqAccess<'de>>(
                self,
                mut sequence: A,
            ) -> Result<Self::Value, A::Error> {
                let mut rows = Vec::new();
                while let Some(row) = sequence.next_element()? {
                    rows.push(row);
                }
                Ok(Rows(Some(rows)))
            }
            fn visit_map<A: MapAccess<'de>>(self, map: A) -> Result<Self::Value, A::Error> {
                Json::deserialize(de::value::MapAccessDeserializer::new(map))?;
                Ok(Rows(None))
            }
        }
        deserializer.deserialize_any(RowsVisitor)
    }
}
#[derive(Default)]
pub struct Document {
    pub root: Json,
    pub spans: Option<Vec<SpanRecord>>,
    pub locations: Option<Vec<RowRecord>>,
}
impl<'de> Deserialize<'de> for Document {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        struct DocumentVisitor;
        impl<'de> Visitor<'de> for DocumentVisitor {
            type Value = Document;
            fn expecting(&self, formatter: &mut fmt::Formatter) -> fmt::Result {
                formatter.write_str("a JSON value")
            }
            scalar_visits!(Document::default());
            fn visit_seq<A: SeqAccess<'de>>(self, sequence: A) -> Result<Self::Value, A::Error> {
                Json::deserialize(de::value::SeqAccessDeserializer::new(sequence))?;
                Ok(Document::default())
            }
            fn visit_map<A: MapAccess<'de>>(self, mut map: A) -> Result<Self::Value, A::Error> {
                let mut root = serde_json::Map::new();
                let mut spans = None;
                let mut locations = None;
                while let Some(key) = map.next_key::<String>()? {
                    if key == "spans" {
                        spans = map.next_value::<Spans>()?.0;
                    } else if key == "locations" {
                        locations = map.next_value::<Rows>()?.0;
                    } else {
                        root.insert(key, map.next_value::<Json>()?);
                    }
                }
                Ok(Document {
                    root: Json::Object(root),
                    spans,
                    locations,
                })
            }
        }
        deserializer.deserialize_any(DocumentVisitor)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn duplicate_root_span_and_coordinate_keys_use_last_value() {
        let document: Document = serde_json::from_str(r#"{"spans":false,"spans":[{"file_index":false,"file_index":0,"start":{"column":false,"col\u0075mn":7,"extra":false,"extr\u0061":null},"end":{}}]}"#).unwrap();
        let span = &document.spans.unwrap()[0];
        assert_eq!(span.file_index, Json::from(0));
        assert_eq!(span.start.field("column"), &Json::from(7));
        assert_eq!(span.start.field("extra"), &Json::Null);
    }
    #[test]
    fn unknown_fields_and_overwritten_values_are_fully_parsed() {
        for data in [
            r#"{"unknown":1e999}"#,
            r#"{"spans":[{"unknown":1e999}],"spans":[]}"#,
        ] {
            assert!(serde_json::from_str::<Document>(data)
                .err()
                .unwrap()
                .to_string()
                .starts_with("number out of range"));
        }
    }
    #[test]
    fn span_unknown_field_is_retained_for_later_semantic_rejection() {
        let document: Document =
            serde_json::from_str(r#"{"spans":[{"unknown":{"nested":[null,true]}}]}"#).unwrap();
        assert!(document.spans.unwrap()[0]
            .unknown_fields
            .contains("unknown"));
    }
    #[test]
    fn source_records_preserve_coordinate_extension_values() {
        let coordinate: Coordinate = serde_json::from_str(
            r#"{"offset":0,"line":1,"column":2,"extra":[null,{"κ":"é界"},18446744073709551615]}"#,
        )
        .unwrap();
        let span = SourceSpan {
            file: Arc::from("exact/λ.lo"),
            start: coordinate.clone(),
            end: coordinate,
        };
        let record = span.source_record();
        assert_eq!(record["file"], "exact/λ.lo");
        assert_eq!(record["start"]["extra"][2].as_u64(), Some(u64::MAX));
        assert_eq!(record["start"], record["end"]);
    }
}
