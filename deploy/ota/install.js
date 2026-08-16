const config = window.coopOTAConfig;
const secureOrigin = `https://${window.location.hostname}:${config.httpsPort}`;
const insecureOrigin = `http://${window.location.hostname}:${config.httpPort}`;

if (window.location.protocol !== "https:") {
  document.documentElement.classList.add("http");
  document.querySelector(".apps").inert = true;
}

document.querySelector("#secure-link").href = secureOrigin;
document.querySelector("#ca-link").href = `${insecureOrigin}/ca.pem`;

for (const button of document.querySelectorAll("[data-manifest]")) {
  button.addEventListener("click", () => {
    const manifestURL = encodeURIComponent(`${secureOrigin}/${button.dataset.manifest}`);
    window.location.href = `itms-services://?action=download-manifest&url=${manifestURL}`;
  });
}
