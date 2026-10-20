#[cfg(target_os = "macos")]
#[derive(Debug)]
pub struct PlatformVerifier {
    pub provider: std::sync::Arc<rustls::crypto::CryptoProvider>,
    pub hostname: String,
}
#[cfg(target_os = "macos")]
impl rustls::client::danger::ServerCertVerifier for PlatformVerifier {
    fn verify_server_cert(
        &self,
        leaf: &rustls::pki_types::CertificateDer<'_>,
        intermediates: &[rustls::pki_types::CertificateDer<'_>],
        server_name: &rustls::pki_types::ServerName<'_>,
        _ocsp: &[u8],
        _now: rustls::pki_types::UnixTime,
    ) -> Result<rustls::client::danger::ServerCertVerified, rustls::Error> {
        use security_framework::{
            certificate::SecCertificate, policy::SecPolicy, secure_transport::SslProtocolSide,
            trust::SecTrust,
        };
        let parsed = rustls::server::ParsedCertificate::try_from(leaf)?;
        rustls::client::verify_server_name(&parsed, server_name)?;
        let mut chain = Vec::with_capacity(intermediates.len() + 1);
        for certificate in std::iter::once(leaf).chain(intermediates) {
            chain.push(SecCertificate::from_der(certificate.as_ref()).map_err(|_| {
                rustls::Error::InvalidCertificate(rustls::CertificateError::BadEncoding)
            })?);
        }
        let policy = SecPolicy::create_ssl(SslProtocolSide::SERVER, Some(&self.hostname));
        let trust = SecTrust::create_with_certificates(&chain, &[policy]).map_err(|_| {
            rustls::Error::General("platform certificate verification failed".into())
        })?;
        trust.evaluate_with_error().map_err(|_| {
            rustls::Error::General("platform certificate verification failed".into())
        })?;
        Ok(rustls::client::danger::ServerCertVerified::assertion())
    }
    fn verify_tls12_signature(
        &self,
        message: &[u8],
        certificate: &rustls::pki_types::CertificateDer<'_>,
        signature: &rustls::DigitallySignedStruct,
    ) -> Result<rustls::client::danger::HandshakeSignatureValid, rustls::Error> {
        rustls::crypto::verify_tls12_signature(
            message,
            certificate,
            signature,
            &self.provider.signature_verification_algorithms,
        )
    }
    fn verify_tls13_signature(
        &self,
        message: &[u8],
        certificate: &rustls::pki_types::CertificateDer<'_>,
        signature: &rustls::DigitallySignedStruct,
    ) -> Result<rustls::client::danger::HandshakeSignatureValid, rustls::Error> {
        rustls::crypto::verify_tls13_signature(
            message,
            certificate,
            signature,
            &self.provider.signature_verification_algorithms,
        )
    }
    fn supported_verify_schemes(&self) -> Vec<rustls::SignatureScheme> {
        self.provider
            .signature_verification_algorithms
            .supported_schemes()
    }
}
