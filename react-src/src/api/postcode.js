
function postcodes(query) {

    return fetch('https://dev.yellow.openware.work/tokens', {method: 'GET',})
        .then(response => response.json()
            .then(data => ({
                    data: data,
                    status: response.status
                })
            ));
}

export default postcodes
